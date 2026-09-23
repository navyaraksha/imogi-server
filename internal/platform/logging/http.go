package logging

import (
	"log/slog"
	"net/http"
	"time"
)

// Middleware emits one structured access event per request. It deliberately
// logs route metadata only; request bodies and sensitive identifiers are never
// included in access logs.
func Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			writer := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(writer, r)
			logger.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", writer.status,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
