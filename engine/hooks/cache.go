package hooks

import (
	"container/list"
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
//
// The cache is a bounded LRU with per-entry TTL: lookups and insertions are
// O(1), and eviction removes the least-recently-used entry rather than scanning
// the whole map.
type CacheHook struct {
	mu    sync.RWMutex
	cache map[string]*list.Element // key -> element in the LRU list
	lru   *list.List               // front = most recently used
	// TTL is how long cached responses remain valid. Default: 5 minutes.
	TTL time.Duration
	// MaxEntries caps the cache size. When exceeded, the least-recently-used
	// entry is evicted. 0 means unlimited.
	MaxEntries int
	// Hits and Misses track cache statistics.
	Hits   int
	Misses int
}

// cacheEntry is the value stored in each LRU list element.
type cacheEntry struct {
	key       string
	output    any
	createdAt time.Time
}

// NewCacheHook creates a response cache with the given TTL.
func NewCacheHook(ttl time.Duration) *CacheHook {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CacheHook{
		cache: make(map[string]*list.Element),
		lru:   list.New(),
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

	h.mu.Lock()
	defer h.mu.Unlock()

	if elem, found := h.cache[key]; found {
		entry := elem.Value.(*cacheEntry)
		if time.Since(entry.createdAt) < h.TTL {
			h.lru.MoveToFront(elem)
			if evt.Metadata == nil {
				evt.Metadata = make(map[string]any)
			}
			evt.Metadata["cache_hit"] = true
			evt.Metadata["cached_response"] = entry.output
			h.Hits++
			return nil
		}
		// Expired: drop it and fall through to a miss.
		h.removeElement(elem)
	}

	h.Misses++
	return nil
}

func (h *CacheHook) After(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallAfter {
		return nil
	}
	if evt.Error != nil || evt.Output == nil {
		return nil
	}

	// Don't re-store responses that were served from cache.
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

	// Update in place if the key already exists.
	if elem, found := h.cache[key]; found {
		entry := elem.Value.(*cacheEntry)
		entry.output = evt.Output
		entry.createdAt = time.Now()
		h.lru.MoveToFront(elem)
		return nil
	}

	elem := h.lru.PushFront(&cacheEntry{
		key:       key,
		output:    evt.Output,
		createdAt: time.Now(),
	})
	h.cache[key] = elem

	// Evict least-recently-used entries beyond the cap.
	for h.MaxEntries > 0 && h.lru.Len() > h.MaxEntries {
		if back := h.lru.Back(); back != nil {
			h.removeElement(back)
		}
	}
	return nil
}

// removeElement removes an element from both the LRU list and the map. Callers
// must hold the write lock.
func (h *CacheHook) removeElement(elem *list.Element) {
	h.lru.Remove(elem)
	delete(h.cache, elem.Value.(*cacheEntry).key)
}

// Clear removes all cached entries.
func (h *CacheHook) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache = make(map[string]*list.Element)
	h.lru = list.New()
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
