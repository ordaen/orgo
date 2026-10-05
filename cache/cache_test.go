package cache

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cacheItem struct {
	Name string
	Tags []string
}

func TestMemory(t *testing.T) {
	c := NewMemory[*cacheItem](0)

	_, ok := c.Get("1")
	assert.False(t, ok)

	c.Set("1", &cacheItem{Name: "john"})
	got, ok := c.Get("1")
	require.True(t, ok)
	assert.Equal(t, "john", got.Name)
	assert.Equal(t, 1, c.Len())

	c.Set("1", &cacheItem{Name: "jane"})
	got, _ = c.Get("1")
	assert.Equal(t, "jane", got.Name)

	c.Delete("1")
	_, ok = c.Get("1")
	assert.False(t, ok)

	c.Set("1", &cacheItem{})
	c.Set("2", &cacheItem{})
	c.Clear()
	assert.Zero(t, c.Len())
}

func TestMemoryCopies(t *testing.T) {
	c := NewMemory[*cacheItem](0)

	item := &cacheItem{Name: "john"}
	c.Set("1", item)
	// changing the stored value does not change the cache
	item.Name = "changed"
	got, _ := c.Get("1")
	assert.Equal(t, "john", got.Name)

	// changing a returned value does not change the cache
	got.Name = "changed"
	got2, _ := c.Get("1")
	assert.Equal(t, "john", got2.Name)
	assert.NotSame(t, got, got2)

	// nil pointers and non-pointer values are stored as they are
	c.Set("nil", nil)
	gotNil, ok := c.Get("nil")
	assert.True(t, ok)
	assert.Nil(t, gotNil)

	values := NewMemory[cacheItem](0)
	values.Set("1", cacheItem{Name: "john"})
	v, _ := values.Get("1")
	assert.Equal(t, "john", v.Name)
}

func TestMemoryTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMemory[*cacheItem](time.Minute)
	c.now = func() time.Time { return now }

	c.Set("1", &cacheItem{Name: "john"})
	now = now.Add(59 * time.Second)
	_, ok := c.Get("1")
	assert.True(t, ok)

	now = now.Add(time.Second)
	_, ok = c.Get("1")
	assert.False(t, ok, "entry must expire after the TTL")
	assert.Zero(t, c.Len(), "expired entry must be removed when it is read")

	// Set restarts the TTL
	c.Set("1", &cacheItem{Name: "jane"})
	now = now.Add(30 * time.Second)
	got, ok := c.Get("1")
	require.True(t, ok)
	assert.Equal(t, "jane", got.Name)
}

func TestMemoryAll(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMemory[*cacheItem](time.Minute)
	c.now = func() time.Time { return now }

	c.Set("1", &cacheItem{Name: "john"})
	now = now.Add(30 * time.Second)
	c.Set("2", &cacheItem{Name: "jane"})
	c.Set("3", &cacheItem{Name: "jack"})

	names := map[string]string{}
	for key, v := range c.All() {
		names[key] = v.Name
	}
	assert.Equal(t, map[string]string{"1": "john", "2": "jane", "3": "jack"}, names)

	// expired entries are skipped
	now = now.Add(30 * time.Second)
	keys := []string{}
	for key := range c.All() {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, []string{"2", "3"}, keys)

	// the cache can be changed during the iteration, and it can be stopped
	n := 0
	for key := range c.All() {
		c.Delete(key)
		c.Set("new", &cacheItem{})
		n++
		break
	}
	assert.Equal(t, 1, n)
	assert.Equal(t, 3, c.Len())

	// the values are the cached values, unlike Get it does not copy them
	c.Clear()
	c.Set("1", &cacheItem{Name: "john"})
	var first, second *cacheItem
	for _, v := range c.All() {
		first = v
	}
	for _, v := range c.All() {
		second = v
	}
	assert.Same(t, first, second)
	got, _ := c.Get("1")
	assert.NotSame(t, first, got)
}

func TestMemoryFind(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMemory[*cacheItem](time.Minute)
	c.now = func() time.Time { return now }
	c.Set("1", &cacheItem{Name: "john", Tags: []string{"a"}})
	now = now.Add(30 * time.Second)
	c.Set("2", &cacheItem{Name: "jane"})
	c.Set("3", &cacheItem{Name: "jack"})

	got, ok := c.Find(func(v *cacheItem) bool { return v.Name == "jane" })
	require.True(t, ok)
	assert.Equal(t, "jane", got.Name)
	_, ok = c.Find(func(v *cacheItem) bool { return v.Name == "nobody" })
	assert.False(t, ok)

	// the found values are copies
	got.Name = "changed"
	_, ok = c.Find(func(v *cacheItem) bool { return v.Name == "changed" })
	assert.False(t, ok)

	all := c.FindAll(func(v *cacheItem) bool { return strings.HasPrefix(v.Name, "j") })
	names := []string{}
	for _, v := range all {
		names = append(names, v.Name)
	}
	assert.ElementsMatch(t, []string{"john", "jane", "jack"}, names)
	all[0].Name = "changed"
	assert.Empty(t, c.FindAll(func(v *cacheItem) bool { return v.Name == "changed" }))
	assert.Equal(t, []*cacheItem{}, c.FindAll(func(*cacheItem) bool { return false }), "no match is an empty slice")

	// expired entries are skipped
	now = now.Add(30 * time.Second)
	_, ok = c.Find(func(v *cacheItem) bool { return v.Name == "john" })
	assert.False(t, ok)
	assert.Len(t, c.FindAll(func(*cacheItem) bool { return true }), 2)
}

func TestMemoryConcurrent(t *testing.T) {
	c := NewMemory[*cacheItem](time.Millisecond)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			key := strconv.Itoa(i % 5)
			c.Set(key, &cacheItem{Name: key})
			if got, ok := c.Get(key); ok {
				got.Name = "changed" // returned copies can be changed without races
			}
			c.Delete(strconv.Itoa((i + 1) % 5))
			for _, v := range c.All() {
				_ = v.Name
			}
			c.Find(func(v *cacheItem) bool { return v.Name == key })
			c.FindAll(func(v *cacheItem) bool { return v.Name != key })
		})
	}
	wg.Wait()
}
