package repo

import (
	"context"
	"fmt"
	"reflect"

	"github.com/ordaen/orgo/cache"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
)

var (
	_ Repository[model.Model] = (*Cached[model.Model])(nil)
	_ pg.SetupRepository      = (*Cached[model.Model])(nil)
)

// Cached is a Repository that keeps records in a cache by their ID.
//
// FindByID reads from the cache, on a miss it loads the record from the database and caches it.
// FindCached and FindManyCached only read the cache. Create and Update cache the returned record, Delete removes it.
// FindWhere, FindMany and Query always query the database and do not change the cache.
// A failed Update or Delete removes the record from the cache.
//
// The changes made through the repository and its copies are ordered with the cache: a record read from the database
// is not cached when the repository changed it in the meantime.
// Changes made to the table by other processes or by queries outside the repository are not seen by the cache
// until the cached record expires, so use a cache with a TTL when that can happen. With a TTL FindCached and
// FindManyCached do not see the expired records until they are loaded again.
// A cache hit does not check the context.
//
// It can be embedded in a repository type initialized by RegisterCached.
type Cached[T model.Model] struct {
	Base[T]
	cache cache.Cache[T]
	// writes is shared by the copies of the repository, like the cache
	writes *writeTracker
}

// NewCached creates a repository for the table of m using c. A nil c is a cache.Memory without TTL.
// The options are the same as for New.
func NewCached[T model.Model](m T, c cache.Cache[T], opts ...Option) *Cached[T] {
	r := &Cached[T]{}
	r.init(m, c, opts)
	return r
}

// cachedInitializer is a repository embedding Cached, initialized by RegisterCached.
type cachedInitializer interface {
	initializer
	pg.SetupRepository
}

// RegisterCached initializes r, a repository type embedding Cached, with a cache.Memory without TTL,
// registers it with pg.RegisterRepository, so its cache is loaded on connect, and returns it:
//
//	var Users = repo.RegisterCached(&userRepository{}, repo.WithEvents())
//
//	type userRepository struct {
//		repo.Cached[*User]
//	}
//
//	func (r *userRepository) FindByEmail(email string) (*User, error) {
//		return r.FindCached(func(u *User) bool { return u.Email == email })
//	}
//
// Cached must be embedded as a value, not as a pointer.
func RegisterCached[R cachedInitializer](r R, opts ...Option) R {
	r.initRepository(opts)
	return pg.RegisterRepository(r)
}

func (r *Cached[T]) initRepository(opts []Option) {
	r.init(newModel[T](), nil, opts)
}

func (r *Cached[T]) init(m T, c cache.Cache[T], opts []Option) {
	if c == nil {
		c = cache.NewMemory[T](0)
	}
	r.Base.init(m, opts)
	r.cache = c
	r.writes = newWriteTracker()
}

// Setup loads all records of the table and stores them in the cache, so FindByID does not query the database for them.
// When the cache has a Clear method, like cache.Memory, it is cleared first, so records deleted from the table are
// removed from the cache. The records written through the repository while Setup runs keep their cached values.
// On error the cache is not changed.
// Setup runs with the repository context, context.Background() unless the repository was created by WithContext.
// It is called by pg.ConnectWithContext for the repositories registered with pg.RegisterRepository.
func (r *Cached[T]) Setup() error {
	since := r.writes.begin()
	defer r.writes.end()

	records, err := r.find(0, "")
	if err != nil {
		return err
	}
	r.writes.queried()

	r.writes.locked(since, func(changed func(string) bool, keys []string) {
		// the records written since Setup began are newer than the loaded ones
		kept := make(map[string]T, len(keys))
		for _, key := range keys {
			if m, ok := r.cache.Get(key); ok {
				kept[key] = m
			}
		}
		if c, ok := r.cache.(interface{ Clear() }); ok {
			c.Clear()
		}
		for _, m := range records {
			if key := cacheKey(m.GetID()); !changed(key) {
				r.cache.Set(key, m)
			}
		}
		for key, m := range kept {
			r.cache.Set(key, m)
		}
	})
	return nil
}

// WithContext returns a copy of the repository running its queries and model hooks with ctx.
// The copy shares the cache, the repository itself is not changed.
func (r *Cached[T]) WithContext(ctx context.Context) Repository[T] {
	return WithContext(r, ctx)
}

// FindByID returns the cached record with the id. When it is not cached, it loads the record from the database
// and caches it. It returns a new model when there is no record with the id or the query fails.
func (r *Cached[T]) FindByID(id any) T {
	key := cacheKey(id)
	if m, ok := r.cache.Get(key); ok {
		return m
	}
	since := r.writes.begin()
	defer r.writes.end()
	m, err := r.load(key, id, since)
	if err != nil {
		r.logFailed("find", err)
		return newModel[T]()
	}
	return m
}

// load reads the record with the id from the database and caches it, unless the key was written since.
func (r *Cached[T]) load(key string, id any, since uint64) (T, error) {
	m, err := r.findOne(whereID, id)
	if err != nil {
		return m, err
	}
	r.writes.queried()
	r.writes.fill(key, since, func() { r.cache.Set(key, m) })
	return m, nil
}

// FindCached returns a cached record matching match, or pg.ErrRecordNotFound. It does not query the database.
// When several records match, any of them is returned. match gets the cached records and must not change them,
// with cache.Memory it must not call the repository either, the cache is locked while it runs.
// The returned record is a copy like the one returned by FindByID.
func (r *Cached[T]) FindCached(match func(T) bool) (T, error) {
	if m, ok := r.cache.Find(match); ok {
		return m, nil
	}
	var zero T
	return zero, pg.ErrRecordNotFound
}

// FindManyCached returns the cached records matching match in no particular order, an empty slice when none match.
// It does not query the database. match gets the cached records and must not change them,
// with cache.Memory it must not call the repository either, the cache is locked while it runs.
// The returned records are copies like the ones returned by FindByID.
func (r *Cached[T]) FindManyCached(match func(T) bool) []T {
	return r.cache.FindAll(match)
}

// Create inserts m like Base.Create and caches the inserted record.
func (r *Cached[T]) Create(m T) (T, error) {
	since := r.writes.begin()
	defer r.writes.end()

	created, err := r.Base.Create(m)
	if err != nil {
		return created, err
	}
	r.writes.queried()
	r.cacheWritten(created, since)
	return created, nil
}

// Update updates m like Base.Update and caches the updated record. On error it removes m from the cache.
func (r *Cached[T]) Update(m T, fields ...string) (T, error) {
	since := r.writes.begin()
	defer r.writes.end()

	updated, err := r.Base.Update(m, fields...)
	r.writes.queried()
	if err != nil {
		r.uncache(m, since)
		return updated, err
	}
	r.cacheWritten(updated, since)
	return updated, nil
}

// Delete deletes m like Base.Delete and removes it from the cache, also on error.
func (r *Cached[T]) Delete(m T) error {
	since := r.writes.begin()
	defer r.writes.end()

	err := r.Base.Delete(m)
	r.writes.queried()
	r.uncache(m, since)
	return err
}

// cacheWritten caches the record written by an operation begun at since. When the record was also written by another
// operation in the meantime, it is not known which write is the last one, so the record is read again.
func (r *Cached[T]) cacheWritten(m T, since uint64) {
	id := m.GetID()
	key := cacheKey(id)
	del := func() { r.cache.Delete(key) }
	if r.writes.write(key, since, func() { r.cache.Set(key, m) }, del) {
		return
	}
	// the read begins after the write was recorded, so it is cached unless another write follows it
	_, _ = r.load(key, id, r.writes.begin())
	r.writes.end()
}

// uncache removes the model written by an operation begun at since from the cache.
// The model may be nil when an operation failed on its validation.
func (r *Cached[T]) uncache(m T, since uint64) {
	if isNil(m) {
		return
	}
	key := cacheKey(m.GetID())
	del := func() { r.cache.Delete(key) }
	r.writes.write(key, since, del, del)
}

// isNil reports whether the model is nil or a nil pointer.
func isNil(m model.Model) bool {
	if m == nil {
		return true
	}
	v := reflect.ValueOf(m)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// cacheKey returns the cache key of the ID, so FindByID(1) and FindByID(model.ID(1)) use the same key.
func cacheKey(id any) string {
	if mid, ok := id.(model.ModelID); ok {
		return mid.String()
	}
	return fmt.Sprint(id)
}
