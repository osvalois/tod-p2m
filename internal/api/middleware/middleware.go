// internal/middleware/middleware.go

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
	"tod-p2m/internal/config"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/sony/gobreaker"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
)

var streamingErrors = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "streaming_errors_total",
		Help: "Total number of streaming errors",
	},
)

// Prometheus metrics for monitoring HTTP requests
var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

// RequestLogger creates a middleware to log detailed information about each request
func RequestLogger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			t1 := time.Now()

			defer func() {
				duration := time.Since(t1)
				if ww.Status() >= 400 || duration > 1*time.Second {
					log.Info().
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Dur("latency", duration).
						Int("status", ww.Status()).
						Msg("Request processed")
				}

				httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, fmt.Sprintf("%d", ww.Status())).Inc()
				httpRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration.Seconds())
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

// Recoverer creates a middleware to recover from panics and log the error
func Recoverer(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := debug.Stack()
					log.Error().
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Interface("error", err).
						Str("stack", string(stack)).
						Msg("Panic recovered")

					streamingErrors.Inc()
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter creates a middleware to limit the request rate
func RateLimiter(cfg *config.Config) func(next http.Handler) http.Handler {
	lmt := tollbooth.NewLimiter(float64(cfg.RateLimit), &limiter.ExpirableOptions{DefaultExpirationTTL: time.Hour})
	lmt.SetIPLookups([]string{"RemoteAddr", "X-Forwarded-For", "X-Real-IP"})

	return func(next http.Handler) http.Handler {
		return tollbooth.LimitHandler(lmt, next)
	}
}

// TimeoutMiddleware creates a middleware to set a maximum timeout for requests
func TimeoutMiddleware(timeout time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/stream" {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)
			done := make(chan bool, 1)

			go func() {
				next.ServeHTTP(w, r)
				done <- true
			}()

			select {
			case <-ctx.Done():
				w.WriteHeader(http.StatusGatewayTimeout)
				fmt.Fprint(w, "Request timed out")
			case <-done:
				return
			}
		})
	}
}

// CorsMiddleware creates a middleware to handle CORS
func CorsMiddleware(cfg *config.Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			for _, allowedOrigin := range cfg.CORSAllowedOrigins {
				if origin == allowedOrigin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "300")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware adds security headers to responses
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		next.ServeHTTP(w, r)
	})
}

// TracingMiddleware adds OpenTracing to requests
func TracingMiddleware(tracer opentracing.Tracer) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			spanCtx, _ := tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
			span := tracer.StartSpan("HTTP "+r.Method+" "+r.URL.Path, opentracing.ChildOf(spanCtx))
			defer span.Finish()

			ctx := opentracing.ContextWithSpan(r.Context(), span)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// CircuitBreakerMiddleware adds a circuit breaker to protect against cascading failures
func CircuitBreakerMiddleware(cfg *config.Config) func(next http.Handler) http.Handler {
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "HTTP",
		MaxRequests: cfg.CircuitBreakerMaxRequests,
		Interval:    cfg.CircuitBreakerInterval,
		Timeout:     cfg.CircuitBreakerTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= cfg.CircuitBreakerMinRequests && failureRatio >= cfg.CircuitBreakerErrorThreshold
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Log state change or send alert
		},
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/stream" {
				next.ServeHTTP(w, r)
				return
			}

			_, err := cb.Execute(func() (interface{}, error) {
				next.ServeHTTP(w, r)
				return nil, nil
			})

			if err != nil {
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			}
		})
	}
}

// StreamingMiddleware configures headers for streaming responses
func StreamingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		next.ServeHTTP(w, r)
	})
}

// CompressMiddleware adds gzip compression to responses
func CompressMiddleware(next http.Handler) http.Handler {
	return middleware.Compress(5, "gzip")(next)
}

// InitTracer initializes Jaeger tracer
func InitTracer(serviceName string) (opentracing.Tracer, error) {
	cfg := jaegercfg.Configuration{
		ServiceName: serviceName,
		Sampler: &jaegercfg.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans: true,
		},
	}
	tracer, _, err := cfg.NewTracer(jaegercfg.Logger(jaeger.StdLogger))
	return tracer, err
}
