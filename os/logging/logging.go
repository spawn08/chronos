// Package logging provides structured JSON-line logging with correlation and
// tenant identifiers carried through context. It is intended for the ChronosOS
// control plane: the HTTP middleware stamps each request with a correlation id
// and execution code logs through the same context so all lines for one
// request can be joined by correlation_id.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/spawn08/chronos/engine/hooks"
)

// Level is a log severity.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// contextKey is unexported to avoid collisions with other packages' context values.
type contextKey string

const (
	correlationIDKey contextKey = "chronos_correlation_id"
)

// Logger emits structured JSON log lines to an io.Writer. It is safe for
// concurrent use.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	min    Level
	now    func() time.Time
	fields map[string]any // static fields added to every line
}

// Option configures a Logger.
type Option func(*Logger)

// WithMinLevel sets the minimum level that will be emitted.
func WithMinLevel(l Level) Option {
	return func(lg *Logger) { lg.min = l }
}

// WithField attaches a static field to every emitted line.
func WithField(k string, v any) Option {
	return func(lg *Logger) { lg.fields[k] = v }
}

// withClock overrides the time source (used by tests).
func withClock(now func() time.Time) Option {
	return func(lg *Logger) { lg.now = now }
}

// New creates a Logger writing JSON lines to w.
func New(w io.Writer, opts ...Option) *Logger {
	lg := &Logger{
		w:      w,
		min:    LevelInfo,
		now:    time.Now,
		fields: make(map[string]any),
	}
	for _, o := range opts {
		o(lg)
	}
	return lg
}

var levelRank = map[Level]int{
	LevelDebug: 0,
	LevelInfo:  1,
	LevelWarn:  2,
	LevelError: 3,
}

func (l *Logger) enabled(lvl Level) bool {
	return levelRank[lvl] >= levelRank[l.min]
}

// Log emits a single structured line at the given level. fields are optional
// key/value pairs; an odd trailing key is recorded under "_MISSING_VALUE".
func (l *Logger) Log(ctx context.Context, lvl Level, msg string, fields ...any) {
	if !l.enabled(lvl) {
		return
	}

	entry := make(map[string]any, len(l.fields)+6)
	for k, v := range l.fields {
		entry[k] = v
	}
	entry["ts"] = l.now().UTC().Format(time.RFC3339Nano)
	entry["level"] = string(lvl)
	entry["msg"] = msg

	if id := CorrelationIDFromContext(ctx); id != "" {
		entry["correlation_id"] = id
	}
	if tenant := TenantFromContext(ctx); tenant != "" {
		entry["tenant"] = tenant
	}

	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			key = fmt.Sprintf("%v", fields[i])
		}
		entry[key] = fields[i+1]
	}
	if len(fields)%2 == 1 {
		entry["_MISSING_VALUE"] = fields[len(fields)-1]
	}

	line, err := marshalStable(entry)
	if err != nil {
		// Fall back to a minimal, always-valid line.
		line = []byte(fmt.Sprintf(`{"level":%q,"msg":%q,"log_error":%q}`, lvl, msg, err.Error()))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(line, '\n'))
}

// Debug logs at debug level.
func (l *Logger) Debug(ctx context.Context, msg string, fields ...any) {
	l.Log(ctx, LevelDebug, msg, fields...)
}

// Info logs at info level.
func (l *Logger) Info(ctx context.Context, msg string, fields ...any) {
	l.Log(ctx, LevelInfo, msg, fields...)
}

// Warn logs at warn level.
func (l *Logger) Warn(ctx context.Context, msg string, fields ...any) {
	l.Log(ctx, LevelWarn, msg, fields...)
}

// Error logs at error level.
func (l *Logger) Error(ctx context.Context, msg string, fields ...any) {
	l.Log(ctx, LevelError, msg, fields...)
}

// marshalStable marshals with sorted keys for deterministic output.
func marshalStable(entry map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(entry))
	for k := range entry {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := make([]byte, 0, 128)
	buf = append(buf, '{')
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key %q: %w", k, err)
		}
		vb, err := json.Marshal(entry[k])
		if err != nil {
			return nil, fmt.Errorf("marshal value for %q: %w", k, err)
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// --- Context helpers ---

// WithCorrelationID returns a context carrying the given correlation id.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// CorrelationIDFromContext returns the correlation id, or "" if none is set.
func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(correlationIDKey).(string); ok {
		return v
	}
	return ""
}

// WithTenant returns a context carrying the given tenant id. It delegates to
// engine/hooks so the tenant set by the HTTP middleware and the tenant read by
// the metrics PrometheusHook share one context key (single source of truth for
// per-tenant attribution across the os and engine layers).
func WithTenant(ctx context.Context, tenant string) context.Context {
	return hooks.WithTenant(ctx, tenant)
}

// TenantFromContext returns the tenant id, or "" if none is set.
func TenantFromContext(ctx context.Context) string {
	return hooks.TenantFromContext(ctx)
}

// NewCorrelationID generates a random hex correlation id.
func NewCorrelationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should not fail; fall back to a time-based id.
		return fmt.Sprintf("corr_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
