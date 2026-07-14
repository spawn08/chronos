package middleware

import (
	"log"
	"net/http"
	"time"
)

// Recovery returns middleware that recovers from panics in downstream handlers,
// logs the panic (if a logger is provided), and responds with 500 instead of
// crashing the server. It is intended to be the outermost middleware in the chain.
func Recovery(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if logger != nil {
						logger.Printf("panic recovered: %s %s: %v", r.Method, r.URL.Path, rec)
					}
					// Best-effort: if the handler already wrote a header this is a
					// no-op with a logged warning from net/http, but the connection
					// stays alive and the server does not crash.
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}` + "\n"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder wraps an http.ResponseWriter to capture the status code while
// preserving optional interfaces (Flusher, deadline control) via Unwrap.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying ResponseWriter so that http.ResponseController
// and http.Flusher (used by SSE streaming) keep working through the wrapper.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// Flush implements http.Flusher by delegating to the underlying writer via
// http.ResponseController. Without this, wrapping a ResponseWriter in
// statusRecorder hides the underlying Flusher, so a `w.(http.Flusher)` check
// (as SSE streaming does) would fail and the stream would return 500.
func (s *statusRecorder) Flush() {
	_ = http.NewResponseController(s.ResponseWriter).Flush()
}

// RequestLogger returns middleware that logs each request's method, path,
// status code, and duration. If logger is nil, logging is skipped.
func RequestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if logger == nil {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
		})
	}
}
