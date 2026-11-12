package handlers

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/cacher"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var eventItemsRepo = repo.New(&item{}, repo.WithEvents())

// CachedItems caches its index, not its show.
type CachedItems struct{ Resource[*item] }

func (c *CachedItems) Setup() {
	c.Path = "/items"
	c.Repo = eventItemsRepo
	c.CacheRecords = true
	c.Action("index", Handle{})
	c.Action("show", Handle{})
	c.Action("update", Handle{})
	c.Action("names", Handle{Root: true, Func: func(x *Ctx) { x.SetData(len(c.Repo.FindMany(""))) }})
}

// CachedItem caches its show.
type CachedItem struct{ Resource[*item] }

func (c *CachedItem) Setup() {
	c.Path = "/item"
	c.Repo = itemsRepo
	c.CacheRecords = true
	c.Action("show", Handle{})
}

// initCache initializes cacher.Cache for the test.
func initCache(t *testing.T) {
	t.Helper()
	require.NoError(t, cacher.Init(filepath.Join(t.TempDir(), "cache.db")))
	t.Cleanup(func() {
		require.NoError(t, cacher.Cache.Close())
		cacher.Cache = nil
	})
}

func newCachedRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()
	reg.Register("admins", &CachedItems{})
	reg.Register("admins", &CachedItem{})
	engine := gin.New()
	reg.Routes("admins", engine)
	return engine
}

// rename renames the item in the database without events.
func rename(t *testing.T, id int, name string) {
	t.Helper()
	_, err := pg.DB.Exec(t.Context(), `UPDATE items SET name = $1 WHERE id = $2`, name, id)
	require.NoError(t, err)
}

func TestCacheIndex(t *testing.T) {
	seedQueryItems(t)
	initCache(t)
	engine := newCachedRouter(t)

	const url = "/items?filter[id]=3&fields=name"
	want := `{"data":[{"id":"3","name":"gamma"}],"meta":{"total":1,"limit":30,"offset":0}}`
	assert.JSONEq(t, want, serve(t, engine, "GET", url, "").body)
	rename(t, 3, "gamma2")
	r := serve(t, engine, "GET", url, "")
	assert.Equal(t, 200, r.code)
	assert.Equal(t, jsonContentType, r.header.Get("Content-Type"))
	assert.JSONEq(t, want, r.body, "cached")
	assert.Contains(t, serve(t, engine, "GET", url+"&sort=id", "").body, "gamma2", "another URI")

	// an update by the action clears the cache
	r = serve(t, engine, "PATCH", "/items/3", `{"name":"gamma3"}`)
	require.Equal(t, 200, r.code, r.body)
	assert.Contains(t, serve(t, engine, "GET", url, "").body, "gamma3")

	// an update by the repository with events clears the cache
	rec := eventItemsRepo.FindByID(3)
	rec.Name = "gamma4"
	_, err := eventItemsRepo.Update(rec, "name")
	require.NoError(t, err)
	assert.Eventually(t, func() bool {
		return strings.Contains(serve(t, engine, "GET", url, "").body, "gamma4")
	}, time.Second, 5*time.Millisecond)

	// the errors and the other actions are not cached
	assert.Equal(t, 400, serve(t, engine, "GET", "/items?filter[nope]=1", "").code)
	assert.Equal(t, 400, serve(t, engine, "GET", "/items?filter[nope]=1", "").code)
	assert.Equal(t, `{"data":5}`, serve(t, engine, "GET", "/items/names", "").body)
	_, err = pg.DB.Exec(t.Context(), `DELETE FROM items WHERE id = 1`)
	require.NoError(t, err)
	assert.Equal(t, `{"data":4}`, serve(t, engine, "GET", "/items/names", "").body)
}

func TestCacheShow(t *testing.T) {
	seedQueryItems(t)
	initCache(t)
	engine := newCachedRouter(t)

	assert.Equal(t, 404, serve(t, engine, "GET", "/item/77", "").code)
	_, err := pg.DB.Exec(t.Context(), `INSERT INTO items (id, name) VALUES (77, 'x')`)
	require.NoError(t, err)
	assert.Equal(t, 200, serve(t, engine, "GET", "/item/77", "").code, "a 404 is not cached")

	assert.JSONEq(t, `{"data":{"id":"2","name":"beta"}}`, serve(t, engine, "GET", "/item/2?fields=name", "").body)
	rename(t, 2, "beta2")
	assert.JSONEq(t, `{"data":{"id":"2","name":"beta"}}`, serve(t, engine, "GET", "/item/2?fields=name", "").body, "cached")
	assert.JSONEq(t, `{"data":{"id":"2","owner_id":"2"}}`, serve(t, engine, "GET", "/item/2?fields=owner_id", "").body,
		"the fields are in the key")

	// the repository of CachedItem has no events, ClearCache clears the table
	ClearCache("items")
	assert.JSONEq(t, `{"data":{"id":"2","name":"beta2"}}`, serve(t, engine, "GET", "/item/2?fields=name", "").body)
}

func TestNoCacheWithoutCacher(t *testing.T) {
	seedQueryItems(t)
	engine := newCachedRouter(t)
	assert.Contains(t, serve(t, engine, "GET", "/item/2", "").body, "beta")
	rename(t, 2, "beta2")
	assert.Contains(t, serve(t, engine, "GET", "/item/2", "").body, "beta2")
	ClearCache("items")
}

func TestNoCacheAction(t *testing.T) {
	seedQueryItems(t)
	initCache(t)
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()
	reg.Register("admins", &plainResource{setup: func(r *Resource[*item]) {
		r.Path = "/items"
		r.Repo = itemsRepo
		r.CacheRecords = true
		r.Action("show", Handle{NoCache: true})
	}})
	engine := gin.New()
	reg.Routes("admins", engine)
	assert.Contains(t, serve(t, engine, "GET", "/items/2", "").body, "beta")
	rename(t, 2, "beta2")
	assert.Contains(t, serve(t, engine, "GET", "/items/2", "").body, "beta2")
}

type plainResource struct {
	Resource[*item]
	setup func(r *Resource[*item])
}

func (c *plainResource) Setup() { c.setup(&c.Resource) }
