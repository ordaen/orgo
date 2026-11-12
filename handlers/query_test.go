package handlers

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQuery(t *testing.T) {
	v, err := url.ParseQuery("ids=1,%202,,3&limit=10&offset=20&fields=name,ip&sort=name,-created" +
		"&filter[active]=true&query[name]=jo&time[from]=2026-01-02T03:04:05Z&time[column]=updated&other=x")
	require.NoError(t, err)
	q, err := ParseQuery(v)
	require.NoError(t, err)
	from := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	assert.Equal(t, &Query{
		IDs:     []string{"1", "2", "3"},
		Limit:   10,
		Offset:  20,
		Fields:  []string{"name", "ip"},
		Sort:    []SortColumn{{Column: "name"}, {Column: "created", Desc: true}},
		Filters: map[string]string{"active": "true"},
		Queries: map[string]string{"name": "jo"},
		Time:    &TimeRange{From: &from, Column: "updated"},
	}, q)
}

func TestParseQueryEmpty(t *testing.T) {
	q, err := ParseQuery(url.Values{"limit": {""}, "time[from]": {""}})
	require.NoError(t, err)
	assert.Equal(t, &Query{Filters: map[string]string{}, Queries: map[string]string{}}, q)
}

func TestParseQueryErrors(t *testing.T) {
	for _, raw := range []string{
		"limit=x", "limit=-1", "offset=1.5", "sort=-", "time[from]=yesterday", "time[to]=2026-01-02", "time[at]=x",
	} {
		v, err := url.ParseQuery(raw)
		require.NoError(t, err)
		_, err = ParseQuery(v)
		var e Error
		require.ErrorAs(t, err, &e, raw)
		assert.Equal(t, "400", e.Status, raw)
	}
}

func TestSortColumn(t *testing.T) {
	assert.Equal(t, "ip DESC", SortColumn{Column: "ip", Desc: true}.String())
	assert.Equal(t, `"ip" DESC`, SortColumn{Column: "ip", Desc: true}.Expr())
	assert.Equal(t, `"ip"`, SortColumn{Column: "ip"}.Expr())
}

func TestParseSortBy(t *testing.T) {
	cols, ok := parseSortBy("name, created desc")
	assert.True(t, ok)
	assert.Equal(t, []SortColumn{{Column: "name"}, {Column: "created", Desc: true}}, cols)
	for _, s := range []string{"", "lower(name)", "name NULLS FIRST", "a b c", `"name"`} {
		_, ok := parseSortBy(s)
		assert.False(t, ok, s)
	}
}

// seedQueryItems creates the items of the query tests.
func seedQueryItems(t *testing.T) {
	t.Helper()
	require.NoError(t, pg.ClearTables("items"))
	_, err := pg.DB.Exec(context.Background(), `INSERT INTO items (id, name, owner_id, active, ip, created) VALUES
		(1, 'alpha', 1, true, '10.0.0.0/8', '2026-01-01T00:00:00Z'),
		(2, 'beta', 2, false, '10.1.0.0/16', '2026-01-02T00:00:00Z'),
		(3, 'gamma', 1, true, '10.1.2.0/24', '2026-01-03T00:00:00Z'),
		(4, 'delta_x', 3, false, '192.168.0.0/16', '2026-01-04T00:00:00Z'),
		(5, 'alpha', 2, true, '10.0.0.0/16', '2026-01-05T00:00:00Z')`)
	require.NoError(t, err)
}

// queryIDs returns the IDs of the items of the raw URL query, in order.
func queryIDs[T interface {
	*item | *ipItem
	pg.Model
}](t *testing.T, m T, raw string) ([]int, error) {
	t.Helper()
	v, err := url.ParseQuery(raw)
	require.NoError(t, err)
	args, err := ParseQuery(v)
	require.NoError(t, err)
	q, err := ApplyQuery(pg.Query(m), args)
	if err != nil {
		return nil, err
	}
	recs, err := q.Select()
	require.NoError(t, err)
	ids := make([]int, 0, len(recs))
	for _, r := range recs {
		ids = append(ids, int(r.GetID().(model.ID)))
	}
	return ids, nil
}

func TestApplyQuery(t *testing.T) {
	seedQueryItems(t)
	for raw, want := range map[string][]int{
		"":                              {1, 5, 2, 4, 3}, // SortBy name, then id
		"sort=-name":                    {3, 4, 2, 1, 5},
		"sort=-name,-id":                {3, 4, 2, 5, 1},
		"ids=4,2":                       {2, 4},
		"limit=2&offset=1":              {5, 2},
		"filter[active]=true":           {1, 5, 3},
		"filter[active]=false":          {2, 4},
		"filter[owner_id]=1":            {1, 3},
		"filter[owner_id]=2,3":          {5, 2, 4},
		"filter[name]=alpha":            {1, 5},
		"query[name]=ALP":               {1, 5},
		"query[name]=_":                 {4}, // the wildcards are matched literally
		"query[name]=alp&query[ip]=192": {1, 5, 4},
		"time[from]=2026-01-02T00:00:00Z&time[to]=2026-01-04T00:00:00Z":      {2, 3},
		"time[column]=updated&time[from]=2000-01-01T00:00:00Z&filter[id]=3": {3},
	} {
		ids, err := queryIDs(t, &item{}, raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, ids, raw)
	}
}

func TestApplyQueryCustom(t *testing.T) {
	seedQueryItems(t)
	for raw, want := range map[string][]int{
		"":                         {1, 5, 2, 3, 4}, // SortBy ip, by masklen, then id
		"sort=-ip":                 {4, 3, 2, 5, 1},
		"query[ip]=10.1.2.3":       {1, 2, 3}, // the networks containing it
		"query[ip]=10.1.0.0/16":    {1, 2, 3}, // and contained in it
		"query[name]=delta":        {4},
		"query[ip]=192.168.1.1/32": {4},
	} {
		ids, err := queryIDs(t, &ipItem{}, raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, ids, raw)
	}
}

func TestApplyQueryErrors(t *testing.T) {
	for _, raw := range []string{
		"ids=1,x", "filter[nope]=1", `filter[name"]=1`, "query[nope]=1", "sort=nope", "sort=name%3Bdrop",
		"time[column]=nope&time[from]=2026-01-02T00:00:00Z",
	} {
		_, err := queryIDs(t, &item{}, raw)
		var e Error
		require.ErrorAs(t, err, &e, raw)
		assert.Equal(t, "400", e.Status, raw)
	}
}
