// Package cacher caches values in a bbolt database file. The values are addressed by paths: all path elements
// but the last one name the bucket, the last one is the key, a path of one element is a key in a default bucket. Entries expire after the TTL of the Cacher,
// also across restarts, and the expired entries are purged in the background.
package cacher

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"time"
)

// DefaultTTL is the time the entries of the Cache initialized by Init are kept.
const DefaultTTL = 30 * time.Minute

const (
	// metaBucket holds the format version of the stored entries, it cannot be a path bucket name,
	// those do not start with the separator.
	metaBucket = pathSeparator + "cacher"
	// formatVersion is the version of the entry format: the big endian unix nano expiration time and the value.
	// A file with another version is cleared on open.
	formatVersion = "2"
	headerSize    = 8
	// pathSeparator joins the path elements of bucket names, it is not expected in them.
	pathSeparator = "\x1f"
	// defaultBucket holds the keys of the paths of one element.
	defaultBucket = pathSeparator
)

var errEmptyPath = errors.New("cacher: empty path")

// Cache is the cache initialized by Init.
var Cache *Cacher

// Init opens the Cache with the file and DefaultTTL. A previously initialized Cache is closed.
func Init(file string) error {
	c, err := New(file, DefaultTTL)
	if err != nil {
		return err
	}
	if Cache != nil {
		Cache.Close()
	}
	Cache = c
	return nil
}

// Clear deletes the cached records of the names in the scope of the Cache, see Cacher.Clear.
func Clear(scope string, names ...string) error {
	return Cache.Clear(scope, names...)
}

// Cacher caches values in a bbolt database file for a TTL. It is safe for concurrent use.
type Cacher struct {
	db        *DB
	ttl       time.Duration
	now       func() time.Time
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// New opens the cache file, creating it when it does not exist, with entries kept for ttl.
// Expired entries are purged when the cache is opened and then every ttl. The cache must be closed with Close.
func New(file string, ttl time.Duration) (*Cacher, error) {
	if ttl <= 0 {
		return nil, errors.New("cacher: ttl must be positive")
	}
	db, err := OpenDB(file)
	if err != nil {
		return nil, err
	}
	c := &Cacher{db: db, ttl: ttl, now: time.Now, stop: make(chan struct{}), done: make(chan struct{})}
	if err := c.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := c.Purge(); err != nil {
		db.Close()
		return nil, err
	}
	go c.purgeLoop()
	return c, nil
}

// migrate clears the file when its entries are not in the current format.
func (c *Cacher) migrate() error {
	if v, err := c.db.Get(metaBucket, "version"); err == nil && string(v) == formatVersion {
		return nil
	}
	if err := c.db.deleteBucketsExcept(metaBucket); err != nil {
		return err
	}
	return c.db.Set(metaBucket, "version", []byte(formatVersion))
}

func (c *Cacher) purgeLoop() {
	defer close(c.done)
	t := time.NewTicker(c.ttl)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.Purge()
		}
	}
}

// Close stops the purging and closes the database file.
func (c *Cacher) Close() error {
	c.closeOnce.Do(func() {
		close(c.stop)
		<-c.done
		c.closeErr = c.db.Close()
	})
	return c.closeErr
}

// Purge deletes the expired entries.
func (c *Cacher) Purge() error {
	now := c.now().UnixNano()
	return c.db.DeleteFunc(metaBucket, func(v []byte) bool {
		return expired(v, now)
	})
}

// GetFunc returns the cached value of the path. When it is not cached, it returns the result of f,
// caching it when it is not empty.
func (c *Cacher) GetFunc(f func() []byte, path ...string) []byte {
	b, err := c.Get(path...)
	if err == nil {
		return b
	}

	b = f()
	if len(b) > 0 {
		c.Set(b, path...)
	}
	return b
}

// Get returns the cached value of the path, or ErrNotFound when it is not cached or expired.
func (c *Cacher) Get(path ...string) ([]byte, error) {
	bucket, key, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	v, err := c.db.Get(bucket, key)
	if err != nil {
		return nil, err
	}
	if expired(v, c.now().UnixNano()) {
		return nil, ErrNotFound
	}
	return v[headerSize:], nil
}

// Set caches the value of the path for the TTL of the cache.
func (c *Cacher) Set(data []byte, path ...string) error {
	bucket, key, err := parsePath(path)
	if err != nil {
		return err
	}
	v := make([]byte, headerSize+len(data))
	binary.BigEndian.PutUint64(v, uint64(c.now().Add(c.ttl).UnixNano()))
	copy(v[headerSize:], data)
	return c.db.Set(bucket, key, v)
}

// Del deletes the cached value of the path.
func (c *Cacher) Del(path ...string) error {
	bucket, key, err := parsePath(path)
	if err != nil {
		return err
	}
	return c.db.DeleteKey(bucket, key)
}

// DelBucket deletes the cached values of all keys in the bucket of the path, the path has no key.
func (c *Cacher) DelBucket(path ...string) error {
	if len(path) == 0 {
		return errEmptyPath
	}
	return c.db.DeleteBucket(parseBucket(path))
}

// Clear deletes the cached records of the names in the scope: the buckets scope/"recs"/name and scope/"rec"/name.
func (c *Cacher) Clear(scope string, names ...string) error {
	var errs []error
	for _, name := range names {
		errs = append(errs, c.DelBucket(scope, "recs", name), c.DelBucket(scope, "rec", name))
	}
	return errors.Join(errs...)
}

// expired reports whether the entry v expired at now, an entry without the header is expired.
func expired(v []byte, now int64) bool {
	return len(v) < headerSize || int64(binary.BigEndian.Uint64(v)) <= now
}

// parsePath returns the bucket and the key of the path. A path of one element is a key in the default bucket.
func parsePath(path []string) (bucket, key string, err error) {
	switch l := len(path); l {
	case 0:
		return "", "", errEmptyPath
	case 1:
		return defaultBucket, path[0], nil
	default:
		return parseBucket(path[:l-1]), path[l-1], nil
	}
}

func parseBucket(path []string) string {
	return strings.Join(path, pathSeparator)
}
