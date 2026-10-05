package cacher

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCacher returns a cacher of a new file with the clock returned by now, closed at the end of the test.
func newTestCacher(t testing.TB, ttl time.Duration) (*Cacher, *time.Time) {
	c, err := New(filepath.Join(t.TempDir(), "cache.db"), ttl)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	now := time.Now()
	c.now = func() time.Time { return now }
	return c, &now
}

func TestParsePath(t *testing.T) {
	b, k, err := parsePath([]string{"1"})
	require.NoError(t, err)
	assert.Equal(t, defaultBucket, b)
	assert.Equal(t, "1", k)
	b, k, err = parsePath([]string{"1", "2"})
	require.NoError(t, err)
	assert.Equal(t, "1", b)
	assert.Equal(t, "2", k)
	b, k, err = parsePath([]string{"1", "2", "3"})
	require.NoError(t, err)
	assert.Equal(t, "1\x1f2", b)
	assert.Equal(t, "3", k)
	_, _, err = parsePath(nil)
	assert.ErrorIs(t, err, errEmptyPath)
}

func BenchmarkParsePath(b *testing.B) {
	for b.Loop() {
		parsePath([]string{"qweqweewrwewre", "werwerwerwe werwe rw", "werwerwerwerwerwr"})
	}
}

func TestCacheSet(t *testing.T) {
	c, _ := newTestCacher(t, time.Minute)
	b := []byte("qwerty")
	require.NoError(t, c.Set(b, "admins", "qwer", "com", "key"))
	got, err := c.Get("admins", "qwer", "com", "key")
	require.NoError(t, err)
	assert.Equal(t, b, got)

	b[0] = 'x'
	got2, err := c.Get("admins", "qwer", "com", "key")
	require.NoError(t, err)
	assert.Equal(t, []byte("qwerty"), got2, "Set copies the value")

	require.NoError(t, c.Set([]byte("one"), "single"))
	got, err = c.Get("single")
	require.NoError(t, err)
	assert.Equal(t, []byte("one"), got)

	require.NoError(t, c.Set([]byte{}, "empty", "key"))
	got, err = c.Get("empty", "key")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCacheGetMissing(t *testing.T) {
	c, _ := newTestCacher(t, time.Minute)
	_, err := c.Get("bucket", "key")
	assert.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, c.Set([]byte("v"), "bucket", "key"))
	_, err = c.Get("bucket", "other")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = c.Get()
	assert.ErrorIs(t, err, errEmptyPath)
}

// TestPathCollision checks the path elements containing the old "-" separator do not collide.
func TestPathCollision(t *testing.T) {
	c, _ := newTestCacher(t, time.Minute)
	require.NoError(t, c.Set([]byte("1"), "a-b", "c", "key"))
	require.NoError(t, c.Set([]byte("2"), "a", "b-c", "key"))
	got, err := c.Get("a-b", "c", "key")
	require.NoError(t, err)
	assert.Equal(t, []byte("1"), got)
}

func TestCacheExpiration(t *testing.T) {
	c, now := newTestCacher(t, time.Minute)
	require.NoError(t, c.Set([]byte("v1"), "b", "key"))
	*now = now.Add(50 * time.Second)
	require.NoError(t, c.Set([]byte("v2"), "b", "key"))

	// a key set again expires a TTL after the last Set
	*now = now.Add(50 * time.Second)
	got, err := c.Get("b", "key")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), got)

	*now = now.Add(10 * time.Second)
	_, err = c.Get("b", "key")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPurge(t *testing.T) {
	c, now := newTestCacher(t, time.Minute)
	require.NoError(t, c.Set([]byte("old"), "b", "old"))
	*now = now.Add(30 * time.Second)
	require.NoError(t, c.Set([]byte("new"), "b", "new"))
	*now = now.Add(40 * time.Second)
	require.NoError(t, c.Purge())

	_, err := c.db.Get("b", "old")
	assert.ErrorIs(t, err, ErrNotFound, "expired entry is deleted")
	_, err = c.db.Get("b", "new")
	assert.NoError(t, err)
	_, err = c.db.Get(metaBucket, "version")
	assert.NoError(t, err, "meta bucket is not purged")
}

// TestReopen checks the entries are kept across restarts and still expire.
func TestReopen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cache.db")
	c, err := New(file, time.Hour)
	require.NoError(t, err)
	require.NoError(t, c.Set([]byte("kept"), "b", "kept"))
	c.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	require.NoError(t, c.Set([]byte("expired"), "b", "expired"))
	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "Close is idempotent")

	c, err = New(file, time.Hour)
	require.NoError(t, err)
	defer c.Close()
	got, err := c.Get("b", "kept")
	require.NoError(t, err)
	assert.Equal(t, []byte("kept"), got)
	_, err = c.db.Get("b", "expired")
	assert.ErrorIs(t, err, ErrNotFound, "expired entries are purged on open")
}

// TestMigrate checks a file of the old format, without the expiration headers, is cleared on open.
func TestMigrate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cache.db")
	db, err := OpenDB(file)
	require.NoError(t, err)
	require.NoError(t, db.Set("admins-qwer", "key", []byte("old format value")))
	require.NoError(t, db.Close())

	c, err := New(file, time.Hour)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.db.Get("admins-qwer", "key")
	assert.ErrorIs(t, err, ErrNotFound)
	v, err := c.db.Get(metaBucket, "version")
	require.NoError(t, err)
	assert.Equal(t, formatVersion, string(v))
}

func TestDel(t *testing.T) {
	c, _ := newTestCacher(t, time.Minute)
	require.NoError(t, c.Set([]byte("v"), "b", "key"))
	require.NoError(t, c.Del("b", "key"))
	_, err := c.Get("b", "key")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NoError(t, c.Del("b", "key"), "missing key")
	assert.NoError(t, c.Del("missing", "key"), "missing bucket")
}

func TestClear(t *testing.T) {
	c, _ := newTestCacher(t, time.Minute)
	for _, b := range [][]string{{"s", "recs", "users"}, {"s", "rec", "users"}, {"s", "rec", "files"}} {
		require.NoError(t, c.Set([]byte("v"), append(b, "key")...))
	}
	require.NoError(t, c.Clear("s", "users", "missing"))
	_, err := c.Get("s", "recs", "users", "key")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = c.Get("s", "rec", "users", "key")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = c.Get("s", "rec", "files", "key")
	assert.NoError(t, err)
	assert.ErrorIs(t, c.DelBucket(), errEmptyPath)
}

func TestGetFunc(t *testing.T) {
	c, _ := newTestCacher(t, time.Minute)
	calls := 0
	f := func() []byte { calls++; return []byte("computed") }
	assert.Equal(t, []byte("computed"), c.GetFunc(f, "b", "key"))
	assert.Equal(t, []byte("computed"), c.GetFunc(f, "b", "key"))
	assert.Equal(t, 1, calls)

	empty := func() []byte { calls++; return nil }
	assert.Nil(t, c.GetFunc(empty, "b", "empty"))
	assert.Nil(t, c.GetFunc(empty, "b", "empty"))
	assert.Equal(t, 3, calls, "empty results are not cached")
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Init(filepath.Join(dir, "a.db")))
	first := Cache
	require.NoError(t, Init(filepath.Join(dir, "b.db")))
	t.Cleanup(func() { Cache.Close(); Cache = nil })
	assert.NotSame(t, first, Cache)
	_, err := first.Get("b", "key")
	assert.Error(t, err, "the previous Cache is closed")

	require.NoError(t, Cache.Set([]byte("v"), "s", "rec", "users", "key"))
	require.NoError(t, Clear("s", "users"))
	_, err = Cache.Get("s", "rec", "users", "key")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNewInvalid(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "cache.db"), 0)
	assert.Error(t, err)
	_, err = New(filepath.Join(t.TempDir(), "missing", "cache.db"), time.Minute)
	assert.Error(t, err)
}

// TestPurgeLoop checks the expired entries are purged in the background every TTL.
func TestPurgeLoop(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "cache.db"), 50*time.Millisecond)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.Set([]byte("v"), "b", "key"))
	assert.Eventually(t, func() bool {
		_, err := c.db.Get("b", "key")
		return err == ErrNotFound
	}, 2*time.Second, 20*time.Millisecond)
}

func TestConcurrent(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "cache.db"), time.Minute)
	require.NoError(t, err)
	defer c.Close()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 50 {
				key := fmt.Sprint(i, "-", j)
				want := bytes.Repeat([]byte{byte(i)}, j+1)
				assert.NoError(t, c.Set(want, "b", key))
				got, err := c.Get("b", key)
				assert.NoError(t, err)
				assert.Equal(t, want, got)
			}
		})
	}
	wg.Wait()
}

func BenchmarkCacheGet(b *testing.B) {
	c, _ := newTestCacher(b, time.Minute)
	c.Set([]byte("qwerty"), "admins", "qwer", "com", "key")
	for b.Loop() {
		c.Get("admins", "qwer", "com", "key")
	}
}
