package middleware

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
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
	"golang.org/x/time/rate"
)

var (
	streamingErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "streaming_errors_total",
			Help: "Total number of streaming errors",
		},
	)
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
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
		},
		[]string{"method", "path"},
	)
	activeStreams = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_streams",
			Help: "Number of active streaming connections",
		},
	)
)

func RequestLogger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			t1 := time.Now()

			defer func() {
				duration := time.Since(t1)
				status := ww.Status()
				if status == 0 {
					status = 200
				}
				log.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Dur("latency", duration).
					Int("status", status).
					Int64("bytes", int64(ww.BytesWritten())).
					Str("ip", r.RemoteAddr).
					Str("user-agent", r.UserAgent()).
					Msg("Request processed")

				httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, fmt.Sprintf("%d", status)).Inc()
				httpRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration.Seconds())
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

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

func RateLimiter(cfg *config.Config) func(next http.Handler) http.Handler {
	lmt := tollbooth.NewLimiter(float64(cfg.RateLimit), &limiter.ExpirableOptions{DefaultExpirationTTL: time.Hour})
	lmt.SetIPLookups([]string{"X-Forwarded-For", "X-Real-IP", "RemoteAddr"})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpError := tollbooth.LimitByRequest(lmt, w, r)
			if httpError != nil {
				http.Error(w, httpError.Message, httpError.StatusCode)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func TimeoutMiddleware(timeout time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)
			done := make(chan struct{})
			panicChan := make(chan interface{}, 1)

			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}
				}()
				next.ServeHTTP(w, r)
				close(done)
			}()

			select {
			case p := <-panicChan:
				panic(p)
			case <-ctx.Done():
				w.WriteHeader(http.StatusGatewayTimeout)
				fmt.Fprint(w, "Request timed out")
			case <-done:
			}
		})
	}
}

func CorsMiddleware(cfg *config.Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
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

func CircuitBreakerMiddleware(cfg *config.Config, log zerolog.Logger) func(next http.Handler) http.Handler {
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
			log.Info().Str("name", name).Str("from", from.String()).Str("to", to.String()).Msg("Circuit breaker state changed")
		},
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := cb.Execute(func() (interface{}, error) {
				ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
				next.ServeHTTP(ww, r)
				if ww.Status() >= 500 {
					return nil, fmt.Errorf("server error: %d", ww.Status())
				}
				return nil, nil
			})

			if err != nil {
				log.Error().Err(err).Str("path", r.URL.Path).Msg("Circuit breaker: request failed")
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			}
		})
	}
}

func StreamingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("Transfer-Encoding", "chunked")

			activeStreams.Inc()
			defer activeStreams.Dec()

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		next.ServeHTTP(w, r)
	})
}

func AdaptiveRateLimiter(cfg *config.Config) func(next http.Handler) http.Handler {
	var (
		mu       sync.Mutex
		limiter  = rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.RateLimit)
		lastSeen = make(map[string]time.Time)
	)

	go func() {
		for range time.Tick(1 * time.Minute) {
			mu.Lock()
			for ip, t := range lastSeen {
				if time.Since(t) > 5*time.Minute {
					delete(lastSeen, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr

			mu.Lock()
			if _, exists := lastSeen[ip]; !exists {
				limiter = rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.RateLimit)
			}
			lastSeen[ip] = time.Now()
			mu.Unlock()

			if !limiter.Allow() {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func CompressMiddleware(next http.Handler) http.Handler {
	return middleware.Compress(5, "gzip", "br")(next)
}

func InitTracer(serviceName string) (opentracing.Tracer, error) {
	cfg := jaegercfg.Configuration{
		ServiceName: serviceName,
		Sampler: &jaegercfg.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans:            true,
			BufferFlushInterval: 1 * time.Second,
		},
	}
	tracer, closer, err := cfg.NewTracer(jaegercfg.Logger(jaeger.StdLogger))
	if err != nil {
		return nil, err
	}
	go func() {
		<-time.After(5 * time.Second)
		closer.Close()
	}()
	return tracer, nil
}
