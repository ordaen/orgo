package pg

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindPlaceholders(t *testing.T) {
	tests := []struct {
		sql, want string
		offset    int
		n         int
	}{
		{sql: "name = ?", offset: 0, want: "name = $1", n: 1},
		{sql: "name = ? AND age > ?", offset: 3, want: "name = $4 AND age > $5", n: 2},
		{sql: "a = ?::int OR b IN (?, ?)", offset: 1, want: "a = $2::int OR b IN ($3, $4)", n: 3},
		{sql: "active", offset: 5, want: "active", n: 0},
		// ?? is a literal ?, like the jsonb operators
		{sql: "tags ?? ? AND tags ??| ?", offset: 0, want: "tags ? $1 AND tags ?| $2", n: 2},
		// strings, quoted identifiers and comments are not changed
		{sql: "name = '?' AND x = ?", offset: 1, want: "name = '?' AND x = $2", n: 1},
		{sql: `"col?" = ?`, offset: 1, want: `"col?" = $2`, n: 1},
		{sql: "name = 'it''s ?' AND x = ?", offset: 2, want: "name = 'it''s ?' AND x = $3", n: 1},
		{sql: `name = E'it\'s ?' AND x = ?`, offset: 0, want: `name = E'it\'s ?' AND x = $1`, n: 1},
		{sql: "name = $$it's ?$$ AND x = $tag$?$tag$ AND y = ?", offset: 0, want: "name = $$it's ?$$ AND x = $tag$?$tag$ AND y = $1", n: 1},
		{sql: "x = ? -- y = ?\nAND z = ? /* w = ? */", offset: 0, want: "x = $1 -- y = ?\nAND z = $2 /* w = ? */", n: 2},
		// unterminated quote is copied as it is
		{sql: "x = ? AND y = 'abc ?", offset: 1, want: "x = $2 AND y = 'abc ?", n: 1},
	}
	for _, tt := range tests {
		got, _, err := bindPlaceholders(tt.sql, tt.offset, make([]any, tt.n))
		require.NoError(t, err, tt.sql)
		assert.Equal(t, tt.want, got, tt.sql)
	}
}

func TestBindWhere(t *testing.T) {
	sql, args, err := BindWhere("a = ? AND b = ?", 1, "x")
	require.NoError(t, err)
	assert.Equal(t, "a = $1 AND b = $2", sql)
	assert.Equal(t, []any{1, "x"}, args)

	_, _, err = BindWhere("a = $1", 1)
	assert.ErrorContains(t, err, "0 placeholders, 1 args")
	_, _, err = BindWhere("a = ? AND b = ?", 1)
	assert.ErrorContains(t, err, "2 placeholders, 1 args")
}

func TestBindWhereIn(t *testing.T) {
	ids := []int64{1, 2}
	tests := []struct {
		sql, want string
		args      []any
	}{
		{sql: "id IN ?", want: "id = ANY($1)", args: []any{In(ids)}},
		{sql: "id NOT IN ?", want: "id <> ALL($1)", args: []any{In(ids)}},
		{sql: "a = ? AND id not in   ? AND b = ?", want: "a = $1 AND id <> ALL($2) AND b = $3", args: []any{1, In(ids), 2}},
		{sql: "id in\n?", want: "id = ANY($1)", args: []any{In(ids)}},
		{sql: `"NOT" IN ?`, want: `"NOT" = ANY($1)`, args: []any{In(ids)}},
		{sql: "knot IN ?", want: "knot = ANY($1)", args: []any{In(ids)}},
	}
	for _, tt := range tests {
		given := slices.Clone(tt.args)
		got, args, err := BindWhere(tt.sql, tt.args...)
		require.NoError(t, err, tt.sql)
		assert.Equal(t, tt.want, got, tt.sql)
		assert.Contains(t, args, any(ids), "the list is bound as its values")
		assert.Equal(t, given, tt.args, "the args are not modified")
	}

	// a nil list is empty, not NULL, so NOT IN matches every record
	_, args, err := BindWhere("id NOT IN ?", In([]int64(nil)))
	require.NoError(t, err)
	assert.Equal(t, []any{[]int64{}}, args)

	for _, sql := range []string{"id = ?", "id IN (?)", "join ?", "?"} {
		_, _, err := BindWhere(sql, In(ids))
		assert.ErrorContains(t, err, "pg.In arg 1 does not follow IN or NOT IN", sql)
	}
}

func TestQueryBuild(t *testing.T) {
	q := Query(&baseModel{})
	sql, args := q.build("*", true)
	assert.Equal(t, `SELECT * FROM "base_models"`, sql)
	assert.Empty(t, args)

	q = Query(&baseModel{}).
		Where("name = ? OR name = ?", "john", "jane").
		Where("").
		Where("email LIKE ?", "%@example.com").
		Where("id > 0").
		Order("name").
		Order("").
		Order("id DESC").
		Limit(10).
		Offset(20)
	sql, args = q.build("*", true)
	assert.Equal(t, `SELECT * FROM "base_models" WHERE (name = $1 OR name = $2) AND (email LIKE $3) AND (id > 0) ORDER BY name, id DESC LIMIT 10 OFFSET 20`, sql)
	assert.Equal(t, []any{"john", "jane", "%@example.com"}, args)

	sql, _ = q.build("COUNT(*)", false)
	assert.Equal(t, `SELECT COUNT(*) FROM "base_models" WHERE (name = $1 OR name = $2) AND (email LIKE $3) AND (id > 0)`, sql)

	// 0 and negative values remove limit and offset
	sql, _ = q.Limit(0).Offset(-1).build("*", true)
	assert.Equal(t, `SELECT * FROM "base_models" WHERE (name = $1 OR name = $2) AND (email LIKE $3) AND (id > 0) ORDER BY name, id DESC`, sql)

	sql, _ = Query(&builderOrderModel{}).build("*", true)
	assert.Equal(t, `SELECT * FROM "orgo"."order"`, sql)
}

func TestQueryIsImmutable(t *testing.T) {
	base := Query(&baseModel{}).Where("name = ?", "john")
	withEmail := base.Where("email = ?", "a@example.com").Order("id")
	withAge := base.Where("id > ?", 5).Limit(3)

	sql, args := base.build("*", true)
	assert.Equal(t, `SELECT * FROM "base_models" WHERE (name = $1)`, sql)
	assert.Equal(t, []any{"john"}, args)

	sql, args = withEmail.build("*", true)
	assert.Equal(t, `SELECT * FROM "base_models" WHERE (name = $1) AND (email = $2) ORDER BY id`, sql)
	assert.Equal(t, []any{"john", "a@example.com"}, args)

	sql, args = withAge.build("*", true)
	assert.Equal(t, `SELECT * FROM "base_models" WHERE (name = $1) AND (id > $2) LIMIT 3`, sql)
	assert.Equal(t, []any{"john", 5}, args)
}

func TestQueryPlaceholderMismatch(t *testing.T) {
	// a missing arg would shift the args of the next conditions, so it is an error
	q := Query(&baseModel{}).Where("name = ? AND email = ?", "john").Where("id > ?", 5)
	_, err := q.Select()
	require.EqualError(t, err, `[PG] where "name = ? AND email = ?": 2 placeholders, 1 args`)
	_, err = q.First()
	require.Error(t, err)
	_, err = q.Count()
	require.Error(t, err)

	_, err = Query(&baseModel{}).Where("name = 'john'", "extra").Select()
	require.EqualError(t, err, `[PG] where "name = 'john'": 0 placeholders, 1 args`)
}

// seedQuery clears base_models and creates the records john, jane, jack and bob in order.
func seedQuery(t *testing.T) []*baseModel {
	t.Helper()
	require.NoError(t, ClearTables("base_models"))
	res := make([]*baseModel, 0, 4)
	for _, name := range []string{"john", "jane", "jack", "bob"} {
		m, err := CreateModel(&baseModel{Name: name, Email: name + "@example.com"})
		require.NoError(t, err)
		res = append(res, m)
	}
	return res
}

func TestQuerySelect(t *testing.T) {
	seed := seedQuery(t)

	res, err := Query(&baseModel{}).Order("id").Select()
	require.NoError(t, err)
	assert.Equal(t, seed, res)

	res, err = Query(&baseModel{}).
		Where("name LIKE ?", "j%").
		Where("name <> ?", "jane").
		Order("name DESC").
		Select()
	require.NoError(t, err)
	assert.Equal(t, []*baseModel{seed[0], seed[2]}, res)

	res, err = Query(&baseModel{}).Order("id").Limit(2).Offset(1).Select()
	require.NoError(t, err)
	assert.Equal(t, seed[1:3], res)

	res, err = Query(&baseModel{}).Where("name = ?", "missing").Select()
	require.NoError(t, err)
	assert.NotNil(t, res)
	assert.Empty(t, res)

	_, err = Query(&baseModel{}).Where("missing_column = ?", 1).Select()
	require.ErrorContains(t, err, "[PG] select from base_models: ")
}

func TestQueryFirstAndCount(t *testing.T) {
	seed := seedQuery(t)

	first, err := Query(&baseModel{}).Where("name LIKE ?", "j%").Order("id DESC").First()
	require.NoError(t, err)
	assert.Equal(t, seed[2], first)

	_, err = Query(&baseModel{}).Where("name = ?", "missing").First()
	require.ErrorIs(t, err, ErrRecordNotFound)

	n, err := Query(&baseModel{}).Count()
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	// order, limit and offset do not change the count
	n, err = Query(&baseModel{}).Where("name LIKE ?", "j%").Order("id").Limit(1).Offset(1).Count()
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	_, err = Query(&baseModel{}).Where("missing_column = ?", 1).Count()
	require.ErrorContains(t, err, "[PG] count base_models: ")
}

func TestQueryDelete(t *testing.T) {
	setupHookTest(t)
	for _, name := range []string{"john", "jane", "jack", "bob"} {
		_, err := Insert(&hookModel{Name: name})
		require.NoError(t, err)
	}

	q := Query(&hookModel{}).Where("name LIKE ?", "j%").Where("name <> ?", "jane")
	sql, args := q.Order("id").buildDelete()
	assert.Equal(t, `DELETE FROM "base_models" WHERE (name LIKE $1) AND (name <> $2)`, sql)
	assert.Equal(t, []any{"j%", "jane"}, args)

	n, err := q.Order("id").Delete()
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	assert.Empty(t, hookCalls, "the hooks are not called")

	n, err = q.Delete()
	require.NoError(t, err)
	assert.Zero(t, n)

	res, err := Query(&hookModel{}).Order("name").Select()
	require.NoError(t, err)
	require.Len(t, res, 2)
	assert.Equal(t, "bob", res[0].Name)
	assert.Equal(t, "jane", res[1].Name)

	// a delete without a condition or with paging is refused
	_, err = Query(&hookModel{}).Where("").Delete()
	require.EqualError(t, err, "[PG] delete from base_models: no condition")
	_, err = Query(&hookModel{}).Where("id > 0").Limit(1).Delete()
	require.EqualError(t, err, "[PG] delete from base_models: limit and offset are not supported")
	_, err = Query(&hookModel{}).Where("id > 0").Offset(1).Delete()
	require.EqualError(t, err, "[PG] delete from base_models: limit and offset are not supported")
	assert.Equal(t, 2, countRows(t, "base_models"))

	_, err = Query(&hookModel{}).Where("name = ?").Delete()
	require.EqualError(t, err, `[PG] where "name = ?": 1 placeholders, 0 args`)
	_, err = Query(&hookModel{}).Where("missing_column = ?", 1).Delete()
	require.ErrorContains(t, err, "[PG] delete from base_models: ")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Query(&hookModel{}).WithContext(ctx).Where("id > 0").Delete()
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 2, countRows(t, "base_models"))
}

func TestQueryWithContext(t *testing.T) {
	seedQuery(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := Query(&baseModel{})

	_, err := q.WithContext(ctx).Select()
	require.ErrorIs(t, err, context.Canceled)
	_, err = q.WithContext(ctx).First()
	require.ErrorIs(t, err, context.Canceled)
	_, err = q.WithContext(ctx).Count()
	require.ErrorIs(t, err, context.Canceled)

	// the original query has no context
	n, err := q.Count()
	require.NoError(t, err)
	assert.Equal(t, 4, n)
}

func TestQueryConcurrent(t *testing.T) {
	seedQuery(t)
	base := Query(&baseModel{}).Where("name LIKE ?", "j%")

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			res, err := base.Where("id > ?", 0).Order("id").Limit(i + 1).Select()
			if assert.NoError(t, err) {
				assert.Len(t, res, min(i+1, 3))
			}
		})
	}
	wg.Wait()
}
