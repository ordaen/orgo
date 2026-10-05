package repo

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ Repository[*baseModel] = New(&baseModel{})

// seedRecords clears base_models and creates the named records in order.
func seedRecords(t *testing.T, names ...string) []*baseModel {
	t.Helper()
	require.NoError(t, pg.ClearTables("base_models"))
	res := make([]*baseModel, 0, len(names))
	for _, name := range names {
		m, err := pg.CreateModel(&baseModel{Name: name, Email: name + "@example.com"})
		require.NoError(t, err)
		res = append(res, m)
	}
	return res
}

func TestBaseCRUD(t *testing.T) {
	require.NoError(t, pg.ClearTables("base_models"))
	repo := New(&baseModel{})
	assert.Equal(t, "base_models", repo.TableName())

	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	require.True(t, created.ID.Valid())

	found := repo.FindByID(created.ID)
	assert.Equal(t, created, found)

	found.Name = "jane"
	found.Email = "ignored@example.com"
	updated, err := repo.Update(found, "name")
	require.NoError(t, err)
	assert.Equal(t, "jane", updated.Name)
	assert.Empty(t, updated.Email)

	require.NoError(t, repo.Delete(updated))

	assert.Equal(t, &baseModel{}, repo.FindByID(created.ID))
}

func TestBaseFindByIDNotFound(t *testing.T) {
	seedRecords(t, "john")
	repo := New(&baseModel{})

	// a new model is returned
	found := repo.FindByID(model.ID(999999))
	require.NotNil(t, found)
	assert.Equal(t, &baseModel{}, found)
}

func TestBaseFindWhere(t *testing.T) {
	seed := seedRecords(t, "john", "jane", "jack")
	repo := New(&baseModel{})

	assert.Equal(t, seed[1], repo.FindWhere("name = ?", "jane"))

	// the condition may end with ORDER BY to pick the first record
	assert.Equal(t, seed[2], repo.FindWhere("name LIKE ? ORDER BY id DESC", "j%"))

	// a new model is returned when no record matches or the query fails
	assert.Equal(t, &baseModel{}, repo.FindWhere("name = ?", "missing"))
	assert.Equal(t, &baseModel{}, repo.FindWhere("missing_column = ?", 1))
	assert.Equal(t, &baseModel{}, repo.FindWhere("name = ?"))
}

func TestBaseFindMany(t *testing.T) {
	seed := seedRecords(t, "john", "jane", "jack")
	repo := New(&baseModel{})

	assert.Equal(t, []*baseModel{seed[2], seed[1]}, repo.FindMany("name LIKE ? ORDER BY id DESC", "ja%"))

	// an empty condition returns all records
	assert.Len(t, repo.FindMany(""), 3)

	// an empty slice is returned when no record matches or the query fails
	res := repo.FindMany("name = ?", "missing")
	assert.NotNil(t, res)
	assert.Empty(t, res)
	res = repo.FindMany("missing_column = ?", 1)
	assert.NotNil(t, res)
	assert.Empty(t, res)
}

func TestBaseCount(t *testing.T) {
	seedRecords(t, "john", "jane", "jack")
	repo := New(&baseModel{})

	assert.Equal(t, 3, repo.Count())
	assert.Equal(t, 2, repo.CountWhere("name LIKE ?", "ja%"))

	// an empty condition counts all records
	assert.Equal(t, 3, repo.CountWhere(""))

	// 0 is returned when the query fails
	assert.Zero(t, repo.CountWhere("missing_column = ?", 1))

	// the repository context is used
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Zero(t, repo.WithContext(ctx).Count())
	assert.Zero(t, repo.WithContext(ctx).CountWhere("name = ?", "john"))
}

func TestBaseCallsHooks(t *testing.T) {
	setupHookTest(t)
	repo := New(&hookModel{})

	created, err := repo.Create(&hookModel{Name: "john"})
	require.NoError(t, err)
	created.Name = "jane"
	updated, err := repo.Update(created)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(updated))

	id := created.ID.String()
	assert.Equal(t, []string{
		"before:0", "after:john@example.com",
		"before-update:jane", "after-update:jane@updated.example.com",
		"before-delete:" + id + ":jane", "after-delete:" + id + ":jane",
	}, hookCalls)
}

func TestBaseReservedTableName(t *testing.T) {
	require.NoError(t, pg.ClearTables(testSchema+".order"))

	repo := New(&orderModel{})
	created, err := repo.Create(&orderModel{User: "john"})
	require.NoError(t, err)

	assert.Equal(t, []*orderModel{created}, repo.FindMany(`"user" = ?`, "john"))
}

func TestBaseWithContext(t *testing.T) {
	seed := seedRecords(t, "john")
	repo := New(&baseModel{})

	// an untyped ID works, the default context is context.Background()
	assert.Equal(t, seed[0], repo.FindByID(seed[0].ID))
	assert.Equal(t, seed[0], repo.FindByID(int(seed[0].ID)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := repo.WithContext(ctx)

	assert.Equal(t, &baseModel{}, canceled.FindByID(seed[0].ID))
	assert.Equal(t, &baseModel{}, canceled.FindWhere("name = ?", "john"))
	assert.Empty(t, canceled.FindMany(""))
	_, err := canceled.Create(&baseModel{Name: "jane"})
	require.ErrorIs(t, err, context.Canceled)
	_, err = canceled.Update(seed[0])
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, canceled.Delete(seed[0]), context.Canceled)
	assert.Equal(t, "base_models", canceled.TableName())

	// WithContext returns a copy, the original repository keeps its context
	assert.Equal(t, seed[0], repo.FindByID(seed[0].ID))
	assert.Equal(t, 1, countRows(t, "base_models"))
}

func TestBaseWithContextHooks(t *testing.T) {
	setupCtxTest(t)
	ctx := context.WithValue(context.Background(), ctxKey{}, "request-2")
	repo := New(&ctxModel{}).WithContext(ctx)

	created, err := repo.Create(&ctxModel{Name: "john"})
	require.NoError(t, err)
	_, err = repo.Update(created)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(created))

	assert.Equal(t, []string{
		"before-create:request-2", "after-create:request-2",
		"before-update:request-2", "after-update:request-2",
		"before-delete:request-2", "after-delete:request-2",
	}, ctxHookCalls)

	// without WithContext the hooks get context.Background()
	ctxHookCalls = nil
	_, err = New(&ctxModel{}).Create(&ctxModel{Name: "jane"})
	require.NoError(t, err)
	assert.Equal(t, []string{"before-create:", "after-create:"}, ctxHookCalls)
}

// globalBase is shared like an application level repository variable.
var globalBase = New(&baseModel{})

func TestBaseWithContextConcurrent(t *testing.T) {
	seed := seedRecords(t, "john")

	// half of the goroutines use a canceled context, it must not leak into the other goroutines or globalBase
	const n = 20
	var wg sync.WaitGroup
	found := make([]*baseModel, n)
	for i := range n {
		wg.Go(func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if i%2 == 0 {
				cancel()
			}
			found[i] = globalBase.WithContext(ctx).FindByID(seed[0].ID)
		})
	}
	wg.Wait()

	for i, m := range found {
		if i%2 == 0 {
			assert.Equal(t, &baseModel{}, m, "goroutine %d", i)
		} else {
			assert.Equal(t, seed[0], m, "goroutine %d", i)
		}
	}
	// globalBase itself still has no context
	assert.Nil(t, globalBase.ctx)
	assert.Equal(t, seed[0], globalBase.FindByID(seed[0].ID))
}
func TestBaseQuery(t *testing.T) {
	seed := seedRecords(t, "john", "jane", "jack", "bob")
	repo := New(&baseModel{})

	res, err := repo.Query().Where("name LIKE ?", "j%").Order("id DESC").Limit(2).Select()
	require.NoError(t, err)
	assert.Equal(t, []*baseModel{seed[2], seed[1]}, res)

	n, err := repo.Query().Count()
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	// the query uses the repository context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repo.WithContext(ctx).Query().Select()
	require.ErrorIs(t, err, context.Canceled)
	// and it can still be replaced on the query
	res, err = repo.WithContext(ctx).Query().WithContext(context.Background()).Select()
	require.NoError(t, err)
	assert.Len(t, res, 4)

	// reserved word tables are quoted
	require.NoError(t, pg.ClearTables(testSchema+".order"))
	_, err = New(&orderModel{}).Query().Where(`"user" = ?`, "john").Select()
	require.NoError(t, err)
}

func TestCachedQueryBypassesCache(t *testing.T) {
	repo, cache := newCached(t)
	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	setNameInDB(t, created.ID, "changed in db")

	found, err := repo.Query().Where("id = ?", created.ID).First()
	require.NoError(t, err)
	assert.Equal(t, "changed in db", found.Name)

	// the cache is not changed
	cached, _ := cache.Get(created.ID.String())
	assert.Equal(t, "john", cached.Name)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repo.WithContext(ctx).Query().Count()
	require.ErrorIs(t, err, context.Canceled)
}

func TestBaseLogsErrors(t *testing.T) {
	seedRecords(t, "john")
	repo := New(&baseModel{})

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	logged := func() string {
		defer buf.Reset()
		return buf.String()
	}

	// not found records are not errors
	repo.FindByID(model.ID(999999))
	repo.FindWhere("name = ?", "missing")
	assert.Empty(t, logged())

	// the errors without a query are logged
	repo.FindWhere("name = ?")
	assert.Contains(t, logged(), `"msg":"orgo: find failed","table":"base_models","error":"[PG] where \"name = ?\": 1 placeholders, 0 args"`)
	repo.FindMany("name = ?")
	assert.Contains(t, logged(), `"msg":"orgo: find failed"`)
	repo.CountWhere("name = ?")
	assert.Contains(t, logged(), `"msg":"orgo: count failed"`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo.WithContext(ctx).Count()
	assert.Contains(t, logged(), `"msg":"orgo: count failed","table":"base_models","error":"[PG] count base_models: context canceled"`)
	NewCached(&baseModel{}, nil).WithContext(ctx).FindByID(1)
	assert.Contains(t, logged(), `"msg":"orgo: find failed"`)

	// the database errors are logged only by pg, with their query
	repo.FindMany("missing_column = ?", 1)
	out := logged()
	assert.Contains(t, out, `"msg":"orgo: query failed"`)
	assert.NotContains(t, out, "find failed")
}
