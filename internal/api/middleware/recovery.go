package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog"
)

func Recoverer(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					log.Error().
						Interface("recover", rvr).
						Str("stack", string(debug.Stack())).
						Msg("Panic recovered")

					w.WriteHeader(http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
