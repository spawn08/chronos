package knowledge

import (
	"container/list"
	"sync"
	"time"
)

// queryCacheEntry is a single cached query embedding.
type queryCacheEntry struct {
	key     string
	vector  []float32
	expires time.Time
}

// queryCache is a bounded LRU cache with per-entry TTL for query embeddings.
// It is safe for concurrent use.
type queryCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	ll       *list.List               // front = most recently used
	items    map[string]*list.Element // key -> *list.Element(*queryCacheEntry)
	now      func() time.Time         // injectable clock for tests
}

// newQueryCache creates a bounded LRU+TTL cache. A capacity <= 0 or ttl <= 0
// disables caching (newQueryCache returns nil).
func newQueryCache(capacity int, ttl time.Duration) *queryCache {
	if capacity <= 0 || ttl <= 0 {
		return nil
	}
	return &queryCache{
		capacity: capacity,
		ttl:      ttl,
		ll:       list.New(),
		items:    make(map[string]*list.Element, capacity),
		now:      time.Now,
	}
}

// get returns the cached vector for key, if present and not expired.
func (c *queryCache) get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry, _ := el.Value.(*queryCacheEntry)
	if c.now().After(entry.expires) {
		c.removeElement(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return entry.vector, true
}

// put stores the vector for key, evicting the least-recently-used entry if the
// cache is at capacity.
func (c *queryCache) put(key string, vector []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		entry, _ := el.Value.(*queryCacheEntry)
		entry.vector = vector
		entry.expires = c.now().Add(c.ttl)
		c.ll.MoveToFront(el)
		return
	}

	entry := &queryCacheEntry{key: key, vector: vector, expires: c.now().Add(c.ttl)}
	c.items[key] = c.ll.PushFront(entry)

	if c.ll.Len() > c.capacity {
		if back := c.ll.Back(); back != nil {
			c.removeElement(back)
		}
	}
}

// removeElement drops an element from both the list and the index.
// Callers must hold c.mu.
func (c *queryCache) removeElement(el *list.Element) {
	c.ll.Remove(el)
	if entry, ok := el.Value.(*queryCacheEntry); ok {
		delete(c.items, entry.key)
	}
}

// len reports the number of live entries (including not-yet-evicted expired
// ones). Primarily used by tests.
func (c *queryCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
