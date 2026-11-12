package handlers

import (
	"sync"

	"github.com/ordaen/orgo/cacher"
	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/pg"
)

// responseCache is the cache of the responses of an action: the bucket scope/kind/table of cacher.Cache, the
// responses keyed by the request URI. kind is "recs" for index and "rec" for show, like cacher.Clear.
type responseCache struct {
	scope string
	kind  string
	table string
}

// get returns the cached response of the request URI, nil when it is not cached.
func (rc *responseCache) get(uri string) []byte {
	if cacher.Cache == nil {
		return nil
	}
	b, err := cacher.Cache.Get(rc.scope, rc.kind, rc.table, uri)
	if err != nil {
		return nil
	}
	return b
}

// set caches the response of the request URI.
func (rc *responseCache) set(uri string, b []byte) {
	if cacher.Cache == nil {
		return
	}
	if err := cacher.Cache.Set(b, rc.scope, rc.kind, rc.table, uri); err != nil {
		logs.Error(err, "handlers: cache the response of "+uri)
	}
}

// cachedTables maps the tables with cached responses to the scopes caching them.
var cachedTables = struct {
	sync.Mutex
	scopes map[string]map[string]bool
	subs   map[string]bool
}{scopes: map[string]map[string]bool{}, subs: map[string]bool{}}

// cacheTable records that the responses of the table are cached in the scope, and subscribes once per table to
// the changes of the records of modelType to clear them, see ClearCache.
func cacheTable(scope, table, modelType string) {
	cachedTables.Lock()
	defer cachedTables.Unlock()
	if cachedTables.scopes[table] == nil {
		cachedTables.scopes[table] = map[string]bool{}
	}
	cachedTables.scopes[table][scope] = true
	if cachedTables.subs[table] || modelType == "" {
		return
	}
	cachedTables.subs[table] = true
	clear := func(events.Event) { ClearCache(table) }
	for _, hub := range []*events.Hub{events.Creates, events.Updates, events.Deletes} {
		hub.Sub(modelType, clear)
	}
}

// ClearCache deletes the cached responses of the records of the table in all scopes. The writes of CreateRecord,
// UpdateRecord, DeleteRecord and the repositories created with repo.WithEvents clear them, an action writing
// records of a repository without events clears them with it:
//
//	handlers.ClearCache(models.Networks.TableName())
func ClearCache(table string) {
	if cacher.Cache == nil {
		return
	}
	cachedTables.Lock()
	scopes := make([]string, 0, len(cachedTables.scopes[table]))
	for s := range cachedTables.scopes[table] {
		scopes = append(scopes, s)
	}
	cachedTables.Unlock()
	for _, s := range scopes {
		if err := cacher.Cache.Clear(s, table); err != nil {
			logs.Error(err, "handlers: clear the cached responses of "+table)
		}
	}
}

// modelTypeOf returns the model type of the events of the records of T, see pg.ModelType.
func modelTypeOf[T pg.Model]() string {
	return pg.ModelType(newModel[T]())
}
