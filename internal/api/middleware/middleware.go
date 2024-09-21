// internal/middleware/middleware.go

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
	"tod-p2m/internal/config"

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

// Prometheus metrics for monitoring HTTP requests
var (
	// httpRequestsTotal counts the total number of HTTP requests
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration measures the duration of HTTP requests
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
// It uses zerolog for structured logging and includes Prometheus metrics
func RequestLogger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap the response writer to capture the status code and response size
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// Record the start time of the request
			t1 := time.Now()

			defer func() {
				// Calculate the request duration
				duration := time.Since(t1)

				// Log the request details
				log.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("remote_addr", r.RemoteAddr).
					Str("user_agent", r.UserAgent()).
					Dur("latency", duration).
					Int("status", ww.Status()).
					Int("size", ww.BytesWritten()).
					Msg("Request processed")

				// Update Prometheus metrics
				httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, fmt.Sprintf("%d", ww.Status())).Inc()
				httpRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration.Seconds())
			}()

			// Call the next handler
			next.ServeHTTP(ww, r)
		})
	}
}

// Recoverer creates a middleware to recover from panics and log the error
// This helps prevent the entire application from crashing due to a panic in a single request
func Recoverer(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Capture the stack trace
					stack := debug.Stack()

					// Log the panic details
					log.Error().
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Str("remote_addr", r.RemoteAddr).
						Interface("error", err).
						Str("stack", string(stack)).
						Msg("Panic recovered")

					// Return a 500 Internal Server Error response
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter creates a middleware to limit the request rate
// It uses a token bucket algorithm to enforce a maximum number of requests per second
func RateLimiter(cfg *config.Config) func(next http.Handler) http.Handler {
	// Create a new rate limiter using the configuration
	limiter := rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.RateLimit)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if the request is allowed by the rate limiter
			if !limiter.Allow() {
				// If not allowed, return a 429 Too Many Requests response
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// If allowed, call the next handler
			next.ServeHTTP(w, r)
		})
	}
}

// TimeoutMiddleware creates a middleware to set a maximum timeout for requests
// This helps prevent long-running requests from tying up server resources
func TimeoutMiddleware(timeout time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create a new context with the specified timeout
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			// Create a new request with the timeout context
			r = r.WithContext(ctx)

			// Channel to signal when the handler is done
			done := make(chan bool, 1)

			go func() {
				next.ServeHTTP(w, r)
				done <- true
			}()

			select {
			case <-ctx.Done():
				// If the context is done (timeout occurred), return a 504 Gateway Timeout response
				w.WriteHeader(http.StatusGatewayTimeout)
				fmt.Fprint(w, "Request timed out")
			case <-done:
				// If the handler finished before the timeout, do nothing (the handler already wrote the response)
				return
			}
		})
	}
}

// CorsMiddleware creates a middleware to handle CORS (Cross-Origin Resource Sharing)
// This allows controlled access to resources from different domains
func CorsMiddleware(cfg *config.Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if the origin is in the list of allowed origins
			origin := r.Header.Get("Origin")
			for _, allowedOrigin := range cfg.CORSAllowedOrigins {
				if origin == allowedOrigin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}

			// Set other CORS headers
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "300")

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware adds security headers to responses
// These headers help protect against various web vulnerabilities
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// X-XSS-Protection header helps prevent XSS attacks in older browsers
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// X-Frame-Options header prevents clickjacking attacks
		w.Header().Set("X-Frame-Options", "DENY")

		// X-Content-Type-Options header prevents MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Referrer-Policy header controls how much referrer information should be included with requests
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content-Security-Policy header helps prevent various attacks, including XSS
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;")

		// Strict-Transport-Security header enforces HTTPS
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}

// TracingMiddleware adds OpenTracing to requests
// This allows for distributed tracing across multiple services
func TracingMiddleware(tracer opentracing.Tracer) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract any existing span context from the request headers
			spanCtx, _ := tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))

			// Start a new span for this request
			span := tracer.StartSpan("HTTP "+r.Method+" "+r.URL.Path, opentracing.ChildOf(spanCtx))
			defer span.Finish()

			// Add the span to the request context
			ctx := opentracing.ContextWithSpan(r.Context(), span)
			r = r.WithContext(ctx)

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}

// CircuitBreakerMiddleware adds a circuit breaker to protect against cascading failures
// It helps to prevent system overload when a part of the system is failing
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
			_, err := cb.Execute(func() (interface{}, error) {
				next.ServeHTTP(w, r)
				return nil, nil
			})

			if err != nil {
				// If the circuit breaker is open, return a 503 Service Unavailable response
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			}
		})
	}
}

// CompressMiddleware adds gzip compression to responses
// This can significantly reduce the amount of data transferred over the network
func CompressMiddleware(next http.Handler) http.Handler {
	return middleware.Compress(5, "gzip")(next)
}

// InitTracer initializes Jaeger tracer
// Jaeger is used for distributed tracing in microservices architectures
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
