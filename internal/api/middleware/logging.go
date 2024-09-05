package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

func RequestLogger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			t1 := time.Now()
			defer func() {
				log.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Dur("latency", time.Since(t1)).
					Int("status", ww.Status()).
					Int("size", ww.BytesWritten()).
					Msg("Request")
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
