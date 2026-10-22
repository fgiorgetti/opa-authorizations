package cache

import (
	"sort"
	"sync"
)

// EndpointResult holds the latest OPA evaluation result for a single endpoint.
type EndpointResult struct {
	Name       string
	Namespace  string
	Type       string // "Listener" or "Connector"
	Allowed    bool
	Violations []string
}

// Cache is a thread-safe in-memory store of EndpointResult values.
// The map key is "<namespace>/<name>".
type Cache struct {
	mu      sync.RWMutex
	entries map[string]EndpointResult
}

// New returns an initialised, empty Cache.
func New() *Cache {
	return &Cache{
		entries: make(map[string]EndpointResult),
	}
}

// Set inserts or replaces the entry for result.Namespace/result.Name.
func (c *Cache) Set(result EndpointResult) {
	key := result.Namespace + "/" + result.Name
	c.mu.Lock()
	c.entries[key] = result
	c.mu.Unlock()
}

// Delete removes the entry for the given namespace and name, if it exists.
func (c *Cache) Delete(namespace, name string) {
	key := namespace + "/" + name
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// GetAll returns a snapshot of all entries, sorted by key for stable output.
func (c *Cache) GetAll() []EndpointResult {
	c.mu.RLock()
	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	c.mu.RUnlock()

	sort.Strings(keys)

	c.mu.RLock()
	results := make([]EndpointResult, 0, len(keys))
	for _, k := range keys {
		if r, ok := c.entries[k]; ok {
			results = append(results, r)
		}
	}
	c.mu.RUnlock()

	return results
}
