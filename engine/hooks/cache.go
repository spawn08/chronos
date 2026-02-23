package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CacheHook caches LLM responses for identical requests. It intercepts
// EventModelCallBefore to check the cache and EventModelCallAfter to store
// results. Caching is skipped for streaming requests and tool-call responses.
type CacheHook struct {
	mu    sync.RWMutex
	cache map[string]*cachedResponse
	// TTL is how long cached responses remain valid. Default: 5 minutes.
	TTL time.Duration
	// MaxEntries caps the cache size. When exceeded, oldest entries are evicted.
	// 0 means unlimited.
	MaxEntries int
	// Hits and Misses track cache statistics.
	Hits   int
	Misses int
}

type cachedResponse struct {
	output    any
	createdAt time.Time
}

// NewCacheHook creates a response cache with the given TTL.
func NewCacheHook(ttl time.Duration) *CacheHook {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CacheHook{
		cache: make(map[string]*cachedResponse),
		TTL:   ttl,
	}
}

func (h *CacheHook) Before(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallBefore {
		return nil
	}

	key, ok := h.cacheKey(evt)
	if !ok {
		return nil
	}

	h.mu.RLock()
	entry, found := h.cache[key]
	h.mu.RUnlock()

	if found && time.Since(entry.createdAt) < h.TTL {
		if evt.Metadata == nil {
			evt.Metadata = make(map[string]any)
		}
		evt.Metadata["cache_hit"] = true
		evt.Metadata["cached_response"] = entry.output
		h.mu.Lock()
		h.Hits++
		h.mu.Unlock()
		return nil
	}

	h.mu.Lock()
	h.Misses++
	h.mu.Unlock()
	return nil
}

func (h *CacheHook) After(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallAfter {
		return nil
	}
	if evt.Error != nil || evt.Output == nil {
		return nil
	}

	// Don't cache tool-call responses
	if evt.Metadata != nil {
		if _, hit := evt.Metadata["cache_hit"]; hit {
			return nil
		}
	}

	key, ok := h.cacheKey(evt)
	if !ok {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.MaxEntries > 0 && len(h.cache) >= h.MaxEntries {
		h.evictOldest()
	}

	h.cache[key] = &cachedResponse{
		output:    evt.Output,
		createdAt: time.Now(),
	}
	return nil
}

// Clear removes all cached entries.
func (h *CacheHook) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache = make(map[string]*cachedResponse)
	h.Hits = 0
	h.Misses = 0
}

// Stats returns cache hit/miss counts.
func (h *CacheHook) Stats() (hits, misses int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.Hits, h.Misses
}

// cacheKey computes a deterministic key from the model call event. Returns
// false if the request should not be cached (e.g. streaming).
func (h *CacheHook) cacheKey(evt *Event) (string, bool) {
	if evt.Input == nil {
		return "", false
	}

	// Check for streaming — skip cache
	type streamChecker interface {
		IsStream() bool
	}
	if sc, ok := evt.Input.(streamChecker); ok && sc.IsStream() {
		return "", false
	}

	data, err := json.Marshal(evt.Input)
	if err != nil {
		return "", false
	}

	hash := sha256.Sum256(data)
	return fmt.Sprintf("%s:%x", evt.Name, hash), true
}

func (h *CacheHook) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range h.cache {
		if oldestKey == "" || v.createdAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.createdAt
		}
	}
	if oldestKey != "" {
		delete(h.cache, oldestKey)
	}
}
