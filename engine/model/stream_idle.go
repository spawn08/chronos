package model

import (
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// defaultStreamIdleTimeout bounds the silence between bytes of a streaming
// response. Streams have no overall deadline, so without it a stalled
// connection would block a turn forever. Providers send keepalive pings and
// reasoning deltas well within this window.
const defaultStreamIdleTimeout = 5 * time.Minute

// ErrStreamIdle matches (via errors.Is) the error returned when a streaming
// response sends nothing for longer than its idle timeout.
var ErrStreamIdle = errors.New("stream idle timeout")

// streamIdleError is a net.Error timeout so callers that retry transient
// network failures also retry a stalled stream.
type streamIdleError struct{ after time.Duration }

func (e *streamIdleError) Error() string {
	return fmt.Sprintf("%s: no data received for %s", ErrStreamIdle, e.after)
}
func (e *streamIdleError) Is(target error) bool { return target == ErrStreamIdle }
func (e *streamIdleError) Timeout() bool        { return true }
func (e *streamIdleError) Temporary() bool      { return true }

// idleTimeoutBody closes the wrapped body when no bytes arrive for timeout,
// which unblocks a pending Read, and reports that as a streamIdleError.
type idleTimeoutBody struct {
	body     io.ReadCloser
	timeout  time.Duration
	timer    *time.Timer
	timedOut atomic.Bool
}

func newIdleTimeoutBody(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	b := &idleTimeoutBody{body: body, timeout: timeout}
	b.timer = time.AfterFunc(timeout, func() {
		b.timedOut.Store(true)
		_ = body.Close()
	})
	return b
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 && !b.timedOut.Load() {
		b.timer.Reset(b.timeout)
	}
	if err != nil && err != io.EOF && b.timedOut.Load() {
		return n, &streamIdleError{after: b.timeout}
	}
	return n, err
}

func (b *idleTimeoutBody) Close() error {
	b.timer.Stop()
	return b.body.Close()
}
