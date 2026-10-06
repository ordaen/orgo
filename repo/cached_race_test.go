package repo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ordaen/orgo/cache"
	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// interleave makes the first database query of repo run fn before the cache is changed, like another goroutine
// writing between the query and the cache change. The queries run by fn do not call it again.
func interleave(repo *Cached[*baseModel], fn func()) {
	var called atomic.Bool
	repo.writes.afterDB = func() {
		if called.CompareAndSwap(false, true) {
			fn()
		}
	}
}

// assertCacheMatchesDB checks that every cached record is the record stored in the database.
func assertCacheMatchesDB(t *testing.T, mc *cache.Memory[*baseModel]) {
	t.Helper()
	stored := New(&baseModel{}).FindMany("")
	byKey := make(map[string]*baseModel, len(stored))
	for _, m := range stored {
		byKey[m.ID.String()] = m
	}
	for key, m := range mc.All() {
		if assert.Contains(t, byKey, key, "record %s is cached but not stored", key) {
			assert.Equal(t, byKey[key], m, "record %s", key)
		}
	}
}

func TestCachedFindByIDDoesNotCacheDeletedRecord(t *testing.T) {
	repo, mc := newCached(t)
	seed := seedRecords(t, "john")

	// the record is deleted after FindByID read it
	interleave(repo, func() {
		require.NoError(t, repo.Delete(seed[0]))
	})
	found := repo.FindByID(seed[0].ID)
	assert.Equal(t, seed[0], found, "the record read before the delete is returned")

	assert.Zero(t, mc.Len(), "the deleted record must not be cached")
	assert.Equal(t, &baseModel{}, repo.FindByID(seed[0].ID))
}

func TestCachedFindByIDDoesNotOverwriteUpdate(t *testing.T) {
	repo, mc := newCached(t)
	seed := seedRecords(t, "john")

	interleave(repo, func() {
		_, err := repo.Update(&baseModel{Base: seed[0].Base, Name: "jane"})
		require.NoError(t, err)
	})
	assert.Equal(t, seed[0], repo.FindByID(seed[0].ID))

	cached, ok := mc.Get(seed[0].ID.String())
	require.True(t, ok)
	assert.Equal(t, "jane", cached.Name)
	assertCacheMatchesDB(t, mc)
}

func TestCachedConcurrentUpdatesOfRecord(t *testing.T) {
	repo, mc := newCached(t)
	seed := seedRecords(t, "john")

	// the first update is written to the database first, but the second one changes the cache first
	interleave(repo, func() {
		_, err := repo.Update(&baseModel{Base: seed[0].Base, Name: "second"})
		require.NoError(t, err)
	})
	_, err := repo.Update(&baseModel{Base: seed[0].Base, Name: "first"})
	require.NoError(t, err)

	cached, ok := mc.Get(seed[0].ID.String())
	require.True(t, ok, "the record is read again when the order of the writes is not known")
	assert.Equal(t, "second", cached.Name)
	assertCacheMatchesDB(t, mc)
}

func TestCachedSetupKeepsConcurrentWrites(t *testing.T) {
	repo, mc := newCached(t)
	seed := seedRecords(t, "john", "jane")

	var jack *baseModel
	interleave(repo, func() {
		require.NoError(t, repo.Delete(seed[0]))
		_, err := repo.Update(&baseModel{Base: seed[1].Base, Name: "janet"})
		require.NoError(t, err)
		jack, err = repo.Create(&baseModel{Name: "jack"})
		require.NoError(t, err)
	})
	require.NoError(t, repo.Setup())

	assert.Equal(t, 2, mc.Len())
	_, ok := mc.Get(seed[0].ID.String())
	assert.False(t, ok, "the record deleted while Setup ran must not be cached")
	cached, _ := mc.Get(seed[1].ID.String())
	assert.Equal(t, "janet", cached.Name)
	cached, _ = mc.Get(jack.ID.String())
	assert.Equal(t, "jack", cached.Name)
	assertCacheMatchesDB(t, mc)
}

func TestCachedConcurrentOperations(t *testing.T) {
	repo, mc := newCached(t)
	seed := seedRecords(t, "r0", "r1", "r2", "r3")
	require.NoError(t, repo.Setup())

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			r := WithContext(repo, context.Background())
			for i := range 30 {
				m := seed[rand.IntN(len(seed))]
				switch rand.IntN(4) {
				case 0:
					_ = r.FindByID(m.ID)
				case 1:
					_, _ = r.Update(&baseModel{Base: m.Base, Name: fmt.Sprintf("g%d-%d", g, i)})
				case 2:
					_ = r.Delete(&baseModel{Base: m.Base})
				case 3:
					// recreate the record with its ID
					_, _ = r.Create(&baseModel{Base: model.Base[model.ID]{ID: m.ID}, Name: "recreated"})
				}
			}
		})
	}
	wg.Wait()

	assertCacheMatchesDB(t, mc)
	assert.Empty(t, repo.writes.written, "the writes are forgotten when no operation is in flight")
}
