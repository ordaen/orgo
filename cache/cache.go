// Package cache defines the Cache used by repo.Cached and Memory, its in-process implementation.
package cache

import (
	"iter"
	"reflect"
	"sync"
	"time"

	"github.com/ordaen/orgo/model"
)

// Cache stores records by key. Implementations must be safe for concurrent use.
type Cache[T any] interface {
	// Get returns the value of the key and whether it is cached.
	Get(key string) (T, bool)
	// Set caches the value of the key, replacing its cached value.
	Set(key string, value T)
	// Delete removes the key. Deleting a key that is not cached does nothing.
	Delete(key string)
	// Find returns a cached value matching match and whether there is one. When several values match,
	// any of them is returned. match gets the cached values and must not change them.
	Find(match func(T) bool) (T, bool)
	// FindAll returns the cached values matching match in no particular order, an empty slice when none match.
	// match gets the cached values and must not change them.
	FindAll(match func(T) bool) []T
}

var _ Cache[model.Model] = (*Memory[model.Model])(nil)

// Memory is an in-process Cache with an optional TTL.
//
// It stores and returns shallow copies of pointer records, so changing a returned record does not change the cache.
// Nested pointers, slices and maps are shared with the cached record and must not be changed.
// Expired entries are removed when they are read.
type Memory[T any] struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]cacheEntry[T]
	now     func() time.Time
}

type cacheEntry[T any] struct {
	value   T
	expires time.Time // zero when the entry does not expire
}

// NewMemory creates a Memory cache. Entries expire after ttl, or never when ttl is 0.
func NewMemory[T any](ttl time.Duration) *Memory[T] {
	return &Memory[T]{
		ttl:     ttl,
		entries: make(map[string]cacheEntry[T]),
		now:     time.Now,
	}
}

// Get returns a copy of the value of the key and whether it is cached. An expired entry is removed and not returned.
func (c *Memory[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()

	if ok && !e.expires.IsZero() && !c.now().Before(e.expires) {
		c.mu.Lock()
		// the entry may have been replaced after the read lock was released
		if cur, exists := c.entries[key]; exists && cur.expires.Equal(e.expires) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		ok = false
	}
	if !ok {
		var zero T
		return zero, false
	}
	return shallowCopy(e.value), true
}

// Set caches a copy of the value of the key, replacing its cached value and restarting its TTL.
func (c *Memory[T]) Set(key string, value T) {
	e := cacheEntry[T]{value: shallowCopy(value)}
	if c.ttl > 0 {
		e.expires = c.now().Add(c.ttl)
	}
	c.mu.Lock()
	c.entries[key] = e
	c.mu.Unlock()
}

// Delete removes the key. Deleting a key that is not cached does nothing.
func (c *Memory[T]) Delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Find returns a copy of a value matching match that is not expired, and whether there is one. When several values
// match, any of them is returned. match is called with the read lock held, so it must not call the cache.
// It gets the cached values, not copies, and must not change them.
func (c *Memory[T]) Find(match func(T) bool) (T, bool) {
	now := c.now()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if e.live(now) && match(e.value) {
			return shallowCopy(e.value), true
		}
	}
	var zero T
	return zero, false
}

// FindAll returns copies of the values matching match that are not expired, in no particular order, an empty slice
// when none match. match is called with the read lock held, so it must not call the cache.
// It gets the cached values, not copies, and must not change them.
func (c *Memory[T]) FindAll(match func(T) bool) []T {
	now := c.now()
	res := []T{}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if e.live(now) && match(e.value) {
			res = append(res, shallowCopy(e.value))
		}
	}
	return res
}

// live reports whether the entry is not expired at now.
func (e cacheEntry[T]) live(now time.Time) bool {
	return e.expires.IsZero() || now.Before(e.expires)
}

// All returns the entries that are not expired, in no particular order. The entries are read when the iteration
// starts, so the cache can be changed during it. The values are the cached values, not copies, and must not be changed,
// use Get for a copy.
func (c *Memory[T]) All() iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
		now := c.now()
		c.mu.RLock()
		entries := make([]entry[T], 0, len(c.entries))
		for key, e := range c.entries {
			if e.live(now) {
				entries = append(entries, entry[T]{key: key, value: e.value})
			}
		}
		c.mu.RUnlock()

		for _, e := range entries {
			if !yield(e.key, e.value) {
				return
			}
		}
	}
}

type entry[T any] struct {
	key   string
	value T
}

// Clear removes all entries.
func (c *Memory[T]) Clear() {
	c.mu.Lock()
	clear(c.entries)
	c.mu.Unlock()
}

// Len returns the number of entries, including expired entries that were not read yet.
func (c *Memory[T]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// shallowCopy returns a copy of the value a non-nil pointer points to. Other values are returned as they are,
// they are already copied by assignment.
func shallowCopy[T any](v T) T {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return v
	}
	c := reflect.New(rv.Elem().Type())
	c.Elem().Set(rv.Elem())
	return c.Interface().(T)
}
