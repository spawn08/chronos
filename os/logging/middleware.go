package logging

import (
	"net/http"
	"time"
)

// Header names used to propagate correlation and tenant identifiers across
// service boundaries.
const (
	HeaderCorrelationID = "X-Correlation-ID"
	HeaderRequestID     = "X-Request-ID"
	HeaderTenant        = "X-Tenant-ID"
)

// Middleware returns an http middleware that ensures every request carries a
// correlation id (reused from the X-Correlation-ID / X-Request-ID header when
// present, otherwise generated), stamps it on the request context and the
// response header, propagates any tenant header into context, and logs the
// completed request as a structured line.
//
// The integrator can attach this to the ChronosOS server; downstream handlers
// and execution code then log through the same context via
// CorrelationIDFromContext / logger methods so all lines for a request share a
// correlation_id.
func Middleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corrID := r.Header.Get(HeaderCorrelationID)
			if corrID == "" {
				corrID = r.Header.Get(HeaderRequestID)
			}
			if corrID == "" {
				corrID = NewCorrelationID()
			}

			ctx := WithCorrelationID(r.Context(), corrID)
			if tenant := r.Header.Get(HeaderTenant); tenant != "" {
				ctx = WithTenant(ctx, tenant)
			}

			w.Header().Set(HeaderCorrelationID, corrID)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))

			if logger != nil {
				logger.Info(ctx, "http_request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", rec.status,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		})
	}
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// Flush implements http.Flusher when the underlying writer supports it, so the
// middleware does not break SSE streaming responses.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
