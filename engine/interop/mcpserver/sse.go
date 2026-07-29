package mcpserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// sseHeartbeat is the idle keepalive interval for an SSE connection, keeping
// intermediaries from closing a quiet stream.
const sseHeartbeat = 15 * time.Second

// maxBodyBytes bounds a single POSTed JSON-RPC request body on the SSE transport.
const maxBodyBytes = 16 << 20 // 16 MiB

// sseSession is one connected SSE client: the server pushes JSON-RPC responses
// onto out, which the GET handler streams to the client.
type sseSession struct {
	out  chan []byte
	done chan struct{}
}

// sseTransport holds the live SSE sessions keyed by id.
type sseTransport struct {
	mu       sync.RWMutex
	sessions map[string]*sseSession
}

func newSSETransport() *sseTransport {
	return &sseTransport{sessions: make(map[string]*sseSession)}
}

// SSEHandler returns an http.Handler implementing the MCP HTTP+SSE transport on
// a single path. A GET opens the event stream and is answered with an `endpoint`
// event naming the URL (this path plus ?session=<id>) to POST JSON-RPC requests
// to; each POST is processed and its response pushed back to that session's
// stream as a `message` event. This mirrors the engine/mcp SSE client.
func (s *Server) SSEHandler() http.Handler {
	t := newSSETransport()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.serveSSEStream(w, r, t)
		case http.MethodPost:
			s.serveSSEMessage(w, r, t)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// serveSSEStream handles the long-lived GET: it registers a session, advertises
// the POST endpoint, and streams responses until the client disconnects.
func (s *Server) serveSSEStream(w http.ResponseWriter, r *http.Request, t *sseTransport) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sess := &sseSession{out: make(chan []byte, 32), done: make(chan struct{})}
	id := t.add(sess)
	defer t.remove(id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Advertise where the client should POST requests (same path, this session).
	fmt.Fprintf(w, "event: endpoint\ndata: %s?session=%s\n\n", r.URL.Path, id)
	flusher.Flush()

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case msg := <-sess.out:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// serveSSEMessage handles a POSTed JSON-RPC request: it dispatches the message
// and pushes any response onto the addressed session's stream.
func (s *Server) serveSSEMessage(w http.ResponseWriter, r *http.Request, t *sseTransport) {
	id := r.URL.Query().Get("session")
	sess, ok := t.get(id)
	if !ok {
		http.Error(w, "unknown or closed session", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	resp, reply := s.HandleMessage(r.Context(), body)
	if reply {
		select {
		case sess.out <- resp:
		case <-sess.done:
			http.Error(w, "session closed", http.StatusGone)
			return
		case <-r.Context().Done():
			return
		}
	}
	// The JSON-RPC response travels on the SSE stream; the POST just acknowledges.
	w.WriteHeader(http.StatusAccepted)
}

func (t *sseTransport) add(sess *sseSession) string {
	id := newSessionID()
	t.mu.Lock()
	t.sessions[id] = sess
	t.mu.Unlock()
	return id
}

func (t *sseTransport) get(id string) (*sseSession, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	sess, ok := t.sessions[id]
	return sess, ok
}

func (t *sseTransport) remove(id string) {
	t.mu.Lock()
	sess, ok := t.sessions[id]
	if ok {
		delete(t.sessions, id)
	}
	t.mu.Unlock()
	if ok {
		close(sess.done)
	}
}

// newSessionID returns a random, unguessable session id.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; fall back to a timestamp-derived id.
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
