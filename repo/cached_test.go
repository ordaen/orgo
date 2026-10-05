package repo

import (
	"context"
	"sync"
	"testing"

	"github.com/ordaen/orgo/cache"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ Repository[*baseModel] = NewCached(&baseModel{}, nil)

// setNameInDB changes the record name bypassing the repository, so the cache does not see it.
func setNameInDB(t *testing.T, id model.ID, name string) {
	t.Helper()
	_, err := pg.DB.Exec(context.Background(), "UPDATE base_models SET name = $1 WHERE id = $2", name, id)
	require.NoError(t, err)
}

func newCached(t *testing.T) (*Cached[*baseModel], *cache.Memory[*baseModel]) {
	t.Helper()
	require.NoError(t, pg.ClearTables("base_models"))
	mc := cache.NewMemory[*baseModel](0)
	return NewCached(&baseModel{}, mc), mc
}

func TestCachedFindByID(t *testing.T) {
	repo, mc := newCached(t)
	seed, err := New(&baseModel{}).Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	assert.Zero(t, mc.Len())

	// a miss loads the record from the database and caches it
	found := repo.FindByID(seed.ID)
	assert.Equal(t, seed, found)
	assert.Equal(t, 1, mc.Len())

	// a hit does not query the database
	setNameInDB(t, seed.ID, "changed in db")
	found = repo.FindByID(seed.ID)
	assert.Equal(t, "john", found.Name)

	// untyped IDs use the same cache entry
	found = repo.FindByID(int(seed.ID))
	assert.Equal(t, "john", found.Name)
	assert.Equal(t, 1, mc.Len())

	// changing a returned record does not change the cache
	found.Name = "not saved"
	found = repo.FindByID(seed.ID)
	assert.Equal(t, "john", found.Name)

	// after the entry is removed the record is loaded again
	mc.Delete(seed.ID.String())
	found = repo.FindByID(seed.ID)
	assert.Equal(t, "changed in db", found.Name)
}

func TestCachedFindByIDNotFoundIsNotCached(t *testing.T) {
	repo, mc := newCached(t)

	assert.Equal(t, &baseModel{}, repo.FindByID(999999))
	assert.Zero(t, mc.Len())

	_, err := pg.DB.Exec(context.Background(), "INSERT INTO base_models (id, name) VALUES (999999, 'john')")
	require.NoError(t, err)
	assert.Equal(t, "john", repo.FindByID(999999).Name)
}

func TestCachedWrites(t *testing.T) {
	repo, mc := newCached(t)

	// Create caches the created record
	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	setNameInDB(t, created.ID, "changed in db")
	found := repo.FindByID(created.ID)
	assert.Equal(t, created, found)

	// Update caches the updated record, as it is returned by the database
	found.Email = "john@example.com"
	updated, err := repo.Update(found, "email")
	require.NoError(t, err)
	assert.Equal(t, "changed in db", updated.Name)
	setNameInDB(t, created.ID, "changed again")
	found = repo.FindByID(created.ID)
	assert.Equal(t, updated, found)

	// Delete removes the record from the cache
	require.NoError(t, repo.Delete(found))
	assert.Zero(t, mc.Len())
	assert.Equal(t, &baseModel{}, repo.FindByID(created.ID))
}

func TestCachedFailedWritesUncache(t *testing.T) {
	repo, mc := newCached(t)
	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)

	// the record is deleted outside the repository, the cache still has it
	_, err = pg.DB.Exec(context.Background(), "DELETE FROM base_models WHERE id = $1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, repo.FindByID(created.ID))

	_, err = repo.Update(created)
	require.ErrorIs(t, err, pg.ErrNoRowsAffected)
	assert.Equal(t, &baseModel{}, repo.FindByID(created.ID), "a failed update must remove the record from the mc")

	created2, err := repo.Create(&baseModel{Name: "jane"})
	require.NoError(t, err)
	_, err = pg.DB.Exec(context.Background(), "DELETE FROM base_models WHERE id = $1", created2.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.Delete(created2), pg.ErrNoRowsAffected)
	assert.Zero(t, mc.Len(), "a failed delete must remove the record from the mc")

	// validation errors do not touch the cache
	_, err = repo.Update(nil)
	require.Error(t, err)
	require.Error(t, repo.Delete(&baseModel{}))
}

func TestCachedFindWhereAndManyBypassCache(t *testing.T) {
	repo, mc := newCached(t)
	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	setNameInDB(t, created.ID, "changed in db")

	assert.Equal(t, "changed in db", repo.FindWhere("id = ?", created.ID).Name)

	res := repo.FindMany("")
	require.Len(t, res, 1)
	assert.Equal(t, "changed in db", res[0].Name)

	// the cache is not changed by them
	cached, _ := mc.Get(created.ID.String())
	assert.Equal(t, "john", cached.Name)
	assert.Equal(t, "base_models", repo.TableName())
}

func TestCachedWithContext(t *testing.T) {
	repo, mc := newCached(t)
	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	missing, err := New(&baseModel{}).Create(&baseModel{Name: "jane"})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := repo.WithContext(ctx)

	// a hit does not check the context, a miss queries the database with it
	assert.Equal(t, created, canceled.FindByID(created.ID))
	assert.Equal(t, &baseModel{}, canceled.FindByID(missing.ID))
	_, err = canceled.Create(&baseModel{Name: "jack"})
	require.ErrorIs(t, err, context.Canceled)

	// the copy shares the cache, the original repository keeps no context
	assert.Equal(t, 1, mc.Len())
	assert.Equal(t, missing, repo.FindByID(missing.ID))
	assert.Equal(t, 2, mc.Len())
}

func TestCachedCallsHooks(t *testing.T) {
	setupHookTest(t)
	repo := NewCached(&hookModel{}, nil)

	created, err := repo.Create(&hookModel{Name: "john"})
	require.NoError(t, err)
	require.NoError(t, repo.Delete(created))
	id := created.ID.String()
	assert.Equal(t, []string{"before:0", "after:john@example.com", "before-delete:" + id + ":john", "after-delete:" + id + ":john"}, hookCalls)
}

// globalCached is shared like an application level repository variable.
var globalCached = NewCached(&baseModel{}, cache.NewMemory[*baseModel](0))

func TestCachedConcurrent(t *testing.T) {
	require.NoError(t, pg.ClearTables("base_models"))
	created, err := globalCached.Create(&baseModel{Name: "john"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := globalCached.WithContext(ctx)
			found := repo.FindByID(created.ID)
			if !assert.Equal(t, created.ID, found.ID) {
				return
			}
			found.Email = "changed" // returned records can be changed without races
			if i%5 == 0 {
				_, err := repo.Update(found, "name")
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()

	found := globalCached.FindByID(created.ID)
	assert.Equal(t, "john", found.Name)
	assert.Empty(t, found.Email)
}

func TestCachedSetup(t *testing.T) {
	repo, mc := newCached(t)

	// an empty table leaves the cache empty
	require.NoError(t, repo.Setup())
	assert.Zero(t, mc.Len())

	base := New(&baseModel{})
	var seed []*baseModel
	for _, name := range []string{"john", "jane", "jack"} {
		m, err := base.Create(&baseModel{Name: name})
		require.NoError(t, err)
		seed = append(seed, m)
	}

	require.NoError(t, repo.Setup())
	assert.Equal(t, 3, mc.Len())

	// all records are served from the cache
	for _, m := range seed {
		setNameInDB(t, m.ID, "changed in db")
	}
	for _, m := range seed {
		assert.Equal(t, m, repo.FindByID(m.ID))
	}

	// Setup reloads the cache: changed records are updated, deleted records are removed
	_, err := pg.DB.Exec(context.Background(), "DELETE FROM base_models WHERE id = $1", seed[0].ID)
	require.NoError(t, err)
	require.NoError(t, repo.Setup())
	assert.Equal(t, 2, mc.Len())
	_, ok := mc.Get(seed[0].ID.String())
	assert.False(t, ok)
	assert.Equal(t, "changed in db", repo.FindByID(seed[1].ID).Name)
}

func TestCachedSetupError(t *testing.T) {
	mc := cache.NewMemory[*missingTableModel](0)
	mc.Set("1", &missingTableModel{Name: "cached"})
	repo := NewCached(&missingTableModel{}, mc)

	err := repo.Setup()
	require.ErrorContains(t, err, "[PG] select from missing_table: ")
	// the cache is not changed on error
	cached, ok := mc.Get("1")
	require.True(t, ok)
	assert.Equal(t, "cached", cached.Name)
}

func TestCachedSetupWithContext(t *testing.T) {
	repo, mc := newCached(t)
	_, err := New(&baseModel{}).Create(&baseModel{Name: "john"})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = repo.WithContext(ctx).(*Cached[*baseModel]).Setup()
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, mc.Len())
}

func TestCachedRegisteredSetupOnConnect(t *testing.T) {
	seed := seedRecords(t, "john", "jane")
	repo := pg.RegisterRepository(NewCached(&baseModel{}, nil))
	assert.Equal(t, seed[0], repo.FindByID(seed[0].ID))

	// the repository is loaded again on connect, even records it did not find by ID yet
	t.Cleanup(func() {
		if !pg.DB.Connected() {
			require.NoError(t, pg.Connect(testConfig()))
		}
	})
	pg.Close()
	require.NoError(t, pg.Connect(testConfig()))
	_, err := pg.DB.Exec(context.Background(), "DELETE FROM base_models")
	require.NoError(t, err)

	for _, m := range seed {
		assert.Equal(t, m, repo.FindByID(m.ID), "record %s must be served from the cache", m.Name)
	}
}
