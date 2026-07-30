package agui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spawn08/chronos/engine/stream"
)

// sessionParams are the request query parameters, in priority order, that name
// the session (AG-UI thread) an SSE client subscribes to.
var sessionParams = []string{"session", "thread", "threadId"}

// Handler returns an http.Handler that streams a session's run as AG-UI events
// over SSE. The session/thread id comes from the request query (see
// sessionParams); a client that omits it subscribes to the broker firehose
// (useful for a dashboard). Each Chronos broker event is translated to zero or
// more AG-UI events and written as `data: <json>` SSE frames, so an
// AG-UI-compatible frontend renders the run with no Chronos-specific glue.
//
// Per-session routing, subscriber caps, and disconnect cleanup are inherited
// from the broker; a heartbeat keeps the connection alive.
func Handler(broker *stream.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		thread := sessionFromRequest(r)
		sub, err := broker.SubscribeTopic(thread)
		if err != nil {
			http.Error(w, "too many subscribers", http.StatusServiceUnavailable)
			return
		}
		defer broker.Unsubscribe(sub.ID)

		runID := r.URL.Query().Get("run")
		if runID == "" {
			runID = thread
		}
		translator := NewTranslator(thread, runID)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Open the AG-UI lifecycle immediately so a client sees a live stream even
		// before the run emits its first event.
		writeEvent(w, flusher, translator.Start())

		// Honor the broker's configured keepalive interval so the two SSE
		// endpoints can't drift; disable it when the broker does.
		var tick <-chan time.Time
		if hb := broker.Heartbeat(); hb > 0 {
			ticker := time.NewTicker(hb)
			defer ticker.Stop()
			tick = ticker.C
		}

		for {
			select {
			case evt, ok := <-sub.C:
				if !ok {
					return
				}
				events := translator.Translate(evt)
				for i := range events {
					writeEvent(w, flusher, events[i])
				}
			case <-tick:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

// writeEvent marshals one AG-UI event and writes it as an SSE `data:` frame,
// flushing so the client receives it immediately. A marshal error skips the
// event rather than corrupting the stream.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// sessionFromRequest resolves the subscribed session/thread from the request
// query, falling back to the empty topic (firehose) when none is given.
func sessionFromRequest(r *http.Request) string {
	q := r.URL.Query()
	for _, p := range sessionParams {
		if v := q.Get(p); v != "" {
			return v
		}
	}
	return ""
}
