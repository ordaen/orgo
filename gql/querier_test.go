package gql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type item struct {
	model.Base[model.ID]
	Name    string
	OwnerID model.ID
	Active  bool
}

func (i *item) TableName() string { return "items" }
func (i *item) SortBy() string    { return "name DESC" }

var items = repo.New(&item{})

// seedItems creates n items named item-00.., owned by 1 or 2, every third one active.
func seedItems(t *testing.T, n int) []*item {
	t.Helper()
	require.NoError(t, pg.ClearTables("items", "logs"))
	var res []*item
	for i := range n {
		it, err := items.Create(&item{Name: fmt.Sprintf("item-%02d", i), OwnerID: model.ID(i%2 + 1), Active: i%3 == 0})
		require.NoError(t, err)
		res = append(res, it)
	}
	return res
}

func names(recs []*item) []string {
	res := make([]string, len(recs))
	for i, r := range recs {
		res[i] = r.Name
	}
	return res
}

func TestFind(t *testing.T) {
	seeded := seedItems(t, 4)
	q := NewQuerier(items)
	ctx := context.Background()

	rec, err := q.Find(ctx, seeded[1].ID)
	require.NoError(t, err)
	assert.Equal(t, "item-01", rec.Name)

	_, err = q.Find(ctx, model.ID(9999))
	assert.ErrorIs(t, err, ErrNotFound)

	// the scopes apply to every query
	owned := q.Where("owner_id = ?", 1)
	_, err = owned.Find(ctx, seeded[1].ID)
	assert.ErrorIs(t, err, ErrNotFound, "item-01 is owned by 2")
	_, err = owned.Find(ctx, seeded[0].ID)
	assert.NoError(t, err)
}

// TestWhereDoesNotChangeQuerier checks a shared querier is not changed by the scopes of a request.
func TestWhereDoesNotChangeQuerier(t *testing.T) {
	seeded := seedItems(t, 2)
	q := NewQuerier(items)
	ctx := context.Background()
	for range 3 {
		_ = q.Where("owner_id = ?", 1).Where("active")
	}
	assert.Empty(t, q.whereScopes)
	_, err := q.Find(ctx, seeded[1].ID)
	assert.NoError(t, err)

	base := q.Where("owner_id = ?", 1)
	a, b := base.Where("active"), base.Where("NOT active")
	assert.Len(t, a.whereScopes, 2)
	assert.Equal(t, "NOT active", b.whereScopes[1].Where, "the copies do not share their scopes")
	assert.Equal(t, "active", a.whereScopes[1].Where)
}

func TestFindExec(t *testing.T) {
	seeded := seedItems(t, 1)
	q := NewQuerier(items)
	var called []string
	rec, err := q.FindExec(context.Background(), seeded[0].ID, func(i *item) error {
		called = append(called, i.Name)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, seeded[0].ID, rec.ID)
	assert.Equal(t, []string{"item-00"}, called)

	_, err = q.FindExec(context.Background(), seeded[0].ID, func(*item) error { return assert.AnError })
	assert.ErrorIs(t, err, assert.AnError)
}

func TestDelete(t *testing.T) {
	seeded := seedItems(t, 2)
	q := NewQuerier(items)
	ctx := context.Background()

	_, err := q.Where("owner_id = ?", 1).Delete(ctx, seeded[1].ID)
	assert.ErrorIs(t, err, ErrNotFound, "not in the scope")
	assert.Equal(t, 2, items.Count())

	rec, err := q.Delete(ctx, seeded[1].ID)
	require.NoError(t, err)
	assert.Equal(t, "item-01", rec.Name)
	assert.Equal(t, 1, items.Count())
	n, err := pg.CountWhere("logs", "action = 'delete' AND owner_id = ?", seeded[1].ID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the deletion is logged")
}

func TestContextCanceled(t *testing.T) {
	seeded := seedItems(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewQuerier(items).Find(ctx, seeded[0].ID)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFindMany(t *testing.T) {
	seedItems(t, 120)
	q := NewQuerier(items)
	ctx := context.Background()

	recs, err := q.FindMany(ctx, nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, recs, DefaultLimit)
	assert.Equal(t, "item-99", recs[0].Name, "sorted by SortBy without sort, names sort as text")

	recs, err = q.FindMany(ctx, new(1000), new(-5), new("name"))
	require.NoError(t, err)
	assert.Len(t, recs, MaxLimit, "the limit is at most MaxLimit")
	assert.Equal(t, "item-00", recs[0].Name, "a negative offset is ignored")

	recs, err = q.FindMany(ctx, new(3), new(2), new("name asc"))
	require.NoError(t, err)
	assert.Equal(t, []string{"item-02", "item-03", "item-04"}, names(recs))

	recs, err = q.Where("owner_id = ?", 2).FindMany(ctx, new(2), nil, new("name"))
	require.NoError(t, err)
	assert.Equal(t, []string{"item-01", "item-03"}, names(recs))
}

func TestSort(t *testing.T) {
	order, err := Sort[*item](new("name desc, id"))
	require.NoError(t, err)
	assert.Equal(t, `"name" DESC, "id"`, order)
	order, err = Sort[*item](nil)
	require.NoError(t, err)
	assert.Empty(t, order, "the querier applies the SortBy of the model")
	order, err = Sort[*item](new("  "))
	require.NoError(t, err)
	assert.Empty(t, order)

	for _, sort := range []string{
		"missing", "name sideways", "name desc nulls", "name;DROP TABLE items", "(SELECT 1)", "name,", "1",
	} {
		_, err := Sort[*item](&sort)
		assert.EqualError(t, err, fmt.Sprintf("invalid sort %q", sort))
	}
}

// TestSortInjection checks the sort of a client is not run as SQL.
func TestSortInjection(t *testing.T) {
	seedItems(t, 3)
	_, err := NewQuerier(items).FindMany(context.Background(), nil, nil, new("name; DELETE FROM items"))
	assert.Error(t, err)
	assert.Equal(t, 3, items.Count())
}

func TestFindManyConds(t *testing.T) {
	seedItems(t, 10)
	q := NewQuerier(items)
	ctx := context.Background()
	find := func(conds ...Where) []string {
		recs, err := q.FindMany(ctx, new(100), nil, new("name"), conds...)
		require.NoError(t, err)
		return names(recs)
	}

	var nilName *string
	var nilOwner *model.ID
	assert.Len(t, find(Cond("name = ?", nilName)), 10, "a nil value is not applied")
	assert.Len(t, find(Cond("name = ?", nil)), 10, "nil is not applied")
	assert.Len(t, find(Cond("name = ?", "")), 10, "a zero value is not applied")
	assert.Equal(t, []string{"item-03"}, find(Cond("name = ?", new("item-03"))))
	assert.Len(t, find(Cond("active = ?", new(false))), 6, "a pointer to false is applied")
	assert.Len(t, find(Cond("name = ? AND owner_id = ?", new("item-03"), nilOwner)), 10,
		"a condition with an unset value is not applied")
	assert.Equal(t, []string{"item-00", "item-06"}, find(FixedCond("active"), Cond("owner_id = ?", new(model.ID(1)))))
	assert.Equal(t, []string{"item-01"}, find(Cond("name LIKE ?", Like(new("m-01")))))
}

func TestLike(t *testing.T) {
	assert.Equal(t, "%abc%", Like("abc"))
	assert.Equal(t, `%50\%\_off\\%`, Like(`50%_off\`), "the wildcards are escaped")
	var nilString *string
	assert.Empty(t, Like(nilString))

	require.NoError(t, pg.ClearTables("items"))
	for _, n := range []string{"50% off", "500 off"} {
		_, err := items.Create(&item{Name: n})
		require.NoError(t, err)
	}
	recs, err := NewQuerier(items).FindMany(context.Background(), nil, nil, nil, Cond("name LIKE ?", Like("50%")))
	require.NoError(t, err)
	assert.Equal(t, []string{"50% off"}, names(recs))
}

func TestCondTime(t *testing.T) {
	from, to := time.Now().Add(-time.Hour), time.Now()
	assert.Equal(t, Where{key: "created between ? and ?", args: []any{from, to}}, CondTime("created", &from, &to))
	assert.Equal(t, Where{key: "created >= ?", args: []any{from}}, CondTime("created", &from, nil))
	assert.Equal(t, Where{key: "created <= ?", args: []any{to}}, CondTime("created", nil, &to))
	assert.False(t, CondTime("created", nil, nil).Applicable())

	seedItems(t, 3)
	future := time.Now().Add(time.Hour)
	recs, err := NewQuerier(items).FindMany(context.Background(), nil, nil, nil, CondTime("created", &from, &future))
	require.NoError(t, err)
	assert.Len(t, recs, 3)
}

func TestFindManyPaginated(t *testing.T) {
	seedItems(t, 25)
	q := NewQuerier(items)
	ctx := context.Background()

	res, err := q.FindManyPaginated(ctx, new(10), new(2), new("name"))
	require.NoError(t, err)
	assert.Len(t, res.Items, 10)
	assert.Equal(t, "item-10", res.Items[0].Name)
	assert.Equal(t, Pagination{Page: 2, Limit: 10, Total: 25, HasNext: true, HasPrevious: true,
		NextCursor: res.Items[9].ID.String()}, res.Pagination)

	res, err = q.FindManyPaginated(ctx, new(10), new(3), new("name"))
	require.NoError(t, err)
	assert.Len(t, res.Items, 5)
	assert.False(t, res.Pagination.HasNext)

	res, err = q.FindManyPaginated(ctx, new(10), new(9), nil)
	require.NoError(t, err)
	assert.Empty(t, res.Items, "a page after the last one is empty")
	assert.Empty(t, res.Pagination.NextCursor)

	res, err = q.Where("owner_id = ?", 1).FindManyPaginated(ctx, nil, nil, nil, Cond("active = ?", new(true)))
	require.NoError(t, err)
	assert.Equal(t, 5, res.Pagination.Total, "the scopes and the conditions are counted")

	_, err = q.FindManyPaginated(ctx, nil, nil, new("bad sort"))
	assert.Error(t, err)
}
