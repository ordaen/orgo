package pg

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mapperStatus string

type mapperJSON struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type mapperAllTypes struct {
	ID       model.ID
	Int      int
	Int32    int32
	Int16    int16
	Int8     int8
	Uint     uint
	Float64  float64
	Float32  float32
	Text     string
	Flag     bool
	Status   mapperStatus
	At       time.Time
	OptText  *string
	OptInt   *int
	Raw      []byte
	Labels   []string
	Doc      mapperJSON
	DocMap   map[string]any
	DocList  []mapperJSON
	Renamed  string `db:"other_name"`
	Skipped  string `db:"-"`
	internal string
}

func queryRows(t *testing.T, sql string, args ...any) Rows {
	t.Helper()
	rows, err := DB.Query(context.Background(), sql, args...)
	require.NoError(t, err)
	return rows
}

func TestMapRowsAllTypes(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rows := queryRows(t, `SELECT
		7::bigint AS id,
		1::bigint AS int, 2::int AS int32, 3::smallint AS int16, 4::smallint AS int8, 5::bigint AS uint,
		1.5::float8 AS float64, 2.5::float4 AS float32,
		'hello'::text AS text, true AS flag, 'active' AS status, $1::timestamptz AS at,
		'opt'::text AS opt_text, 42 AS opt_int,
		'\xdeadbeef'::bytea AS raw, ARRAY['a','b']::text[] AS labels,
		'{"name":"doc","tags":["x"]}'::jsonb AS doc,
		'{"k":"v"}'::jsonb AS doc_map,
		'[{"name":"a"},{"name":"b"}]'::jsonb AS doc_list,
		'renamed' AS other_name, 'nope' AS skipped, 'nope' AS internal,
		'ignored' AS unknown_column`, at)

	res, err := MapRows[mapperAllTypes](rows)
	require.NoError(t, err)
	require.Len(t, res, 1)

	optText, optInt := "opt", 42
	assert.Equal(t, mapperAllTypes{
		ID:      7,
		Int:     1,
		Int32:   2,
		Int16:   3,
		Int8:    4,
		Uint:    5,
		Float64: 1.5,
		Float32: 2.5,
		Text:    "hello",
		Flag:    true,
		Status:  "active",
		At:      at,
		OptText: &optText,
		OptInt:  &optInt,
		Raw:     []byte{0xde, 0xad, 0xbe, 0xef},
		Labels:  []string{"a", "b"},
		Doc:     mapperJSON{Name: "doc", Tags: []string{"x"}},
		DocMap:  map[string]any{"k": "v"},
		DocList: []mapperJSON{{Name: "a"}, {Name: "b"}},
		Renamed: "renamed",
	}, withUTC(res[0]))
	assert.Empty(t, res[0].internal, "unexported fields must not be mapped")
}

func TestMapRowsNulls(t *testing.T) {
	rows := queryRows(t, `SELECT
		NULL::bigint AS int, NULL::float8 AS float64, NULL::text AS text, NULL::bool AS flag,
		NULL::timestamptz AS at, NULL::text AS opt_text, NULL::int AS opt_int,
		NULL::bytea AS raw, NULL::text[] AS labels,
		NULL::jsonb AS doc, NULL::jsonb AS doc_map, NULL::jsonb AS doc_list`)

	res, err := MapRows[mapperAllTypes](rows)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, mapperAllTypes{}, res[0])
}

func TestMapRowsNullDoesNotLeakBetweenRows(t *testing.T) {
	rows := queryRows(t, `SELECT * FROM (VALUES
		(1, 'first'::text, '{"name":"doc"}'::jsonb),
		(2, NULL, NULL)
	) AS v(int, text, doc) ORDER BY int`)

	res, err := MapRows[mapperAllTypes](rows)
	require.NoError(t, err)
	require.Len(t, res, 2)
	assert.Equal(t, "first", res[0].Text)
	assert.Equal(t, "doc", res[0].Doc.Name)
	assert.Equal(t, 2, res[1].Int)
	assert.Empty(t, res[1].Text)
	assert.Empty(t, res[1].Doc.Name)
}

func TestMapRowsPointerAndValueTypes(t *testing.T) {
	const sql = `SELECT i AS int, 'row' || i AS text FROM generate_series(1, 3) AS i`

	values, err := MapRows[mapperAllTypes](queryRows(t, sql))
	require.NoError(t, err)
	require.Len(t, values, 3)

	ptrs, err := MapRows[*mapperAllTypes](queryRows(t, sql))
	require.NoError(t, err)
	require.Len(t, ptrs, 3)

	for i := range 3 {
		assert.Equal(t, i+1, values[i].Int)
		assert.Equal(t, values[i], *ptrs[i])
	}
	// every pointer must be a separate instance
	assert.NotSame(t, ptrs[0], ptrs[1])
}

func TestMapRowsLimited(t *testing.T) {
	const sql = `SELECT i AS int FROM generate_series(1, 10) AS i`

	res, err := MapRowsLimited[mapperAllTypes](queryRows(t, sql), 3)
	require.NoError(t, err)
	require.Len(t, res, 3)
	assert.Equal(t, []int{1, 2, 3}, []int{res[0].Int, res[1].Int, res[2].Int})

	res, err = MapRowsLimited[mapperAllTypes](queryRows(t, sql), 0)
	require.NoError(t, err)
	assert.Len(t, res, 10)

	res, err = MapRowsLimited[mapperAllTypes](queryRows(t, sql), 20)
	require.NoError(t, err)
	assert.Len(t, res, 10)
}

func TestMapRowsEmpty(t *testing.T) {
	res, err := MapRows[mapperAllTypes](queryRows(t, `SELECT 1 AS int WHERE false`))
	require.NoError(t, err)
	assert.Empty(t, res)
}

// MapperEmbedded is exported, so the embedded pointer field is exported and mapped
type MapperEmbedded struct {
	Text string
}

type mapperEmbedded struct {
	Text string
}

type mapperWithEmbeddedPtr struct {
	*mapperEmbedded
	Int int
}

type mapperWithExportedEmbeddedPtr struct {
	*MapperEmbedded
	Int int
}

func TestMapRowsEmbeddedPointer(t *testing.T) {
	res, err := MapRows[mapperWithExportedEmbeddedPtr](queryRows(t, `SELECT 1 AS int, 'embedded' AS text`))
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.NotNil(t, res[0].MapperEmbedded)
	assert.Equal(t, "embedded", res[0].Text)
	assert.Equal(t, 1, res[0].Int)

	// unexported embedded pointers are skipped, not dereferenced
	res2, err := MapRows[mapperWithEmbeddedPtr](queryRows(t, `SELECT 1 AS int, 'embedded' AS text`))
	require.NoError(t, err)
	require.Len(t, res2, 1)
	assert.Nil(t, res2[0].mapperEmbedded)
	assert.Equal(t, 1, res2[0].Int)
}

func TestMapRowsNotStruct(t *testing.T) {
	_, err := MapRows[int](queryRows(t, `SELECT 1`))
	assert.EqualError(t, err, "[PG] map rows: int is not a struct")
}

func TestMapRowsScanError(t *testing.T) {
	_, err := MapRows[mapperAllTypes](queryRows(t, `SELECT 'not a number'::text AS int`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan:")
}

func TestMapRowsInvalidJSON(t *testing.T) {
	// jsonb that does not fit the target struct type
	_, err := MapRows[mapperAllTypes](queryRows(t, `SELECT '"just a string"'::jsonb AS doc`))
	require.Error(t, err)
}

// mapperScanner records how pgx called its sql.Scanner and driver.Valuer methods.
type mapperScanner struct {
	Src   string
	Calls int
}

func (s *mapperScanner) Scan(src any) error {
	s.Calls++
	s.Src = fmt.Sprintf("%T:%v", src, src)
	return nil
}

func (s mapperScanner) Value() (driver.Value, error) {
	return "from-valuer", nil
}

type mapperScannerModel struct {
	Val    mapperScanner
	PtrVal *mapperScanner
	Doc    mapperScanner
}

func TestMapRowsScanner(t *testing.T) {
	rows := queryRows(t, `SELECT * FROM (VALUES
		(1, 'abc'::text, 'x'::text, '{"a":1}'::jsonb),
		(2, NULL, NULL, NULL)
	) AS v(n, val, ptr_val, doc) ORDER BY n`)

	res, err := MapRows[mapperScannerModel](rows)
	require.NoError(t, err)
	require.Len(t, res, 2)

	// non-NULL values go through Scan, jsonb is passed as raw bytes instead of being unmarshaled
	assert.Equal(t, mapperScanner{Src: "string:abc", Calls: 1}, res[0].Val)
	require.NotNil(t, res[0].PtrVal)
	assert.Equal(t, mapperScanner{Src: "string:x", Calls: 1}, *res[0].PtrVal)
	assert.Equal(t, 1, res[0].Doc.Calls)
	assert.Equal(t, fmt.Sprintf("%T:%v", []byte{}, []byte(`{"a": 1}`)), res[0].Doc.Src)

	// NULL leaves value fields at their zero value without calling Scan(nil), pointer fields stay nil
	assert.Equal(t, mapperScanner{}, res[1].Val)
	assert.Equal(t, mapperScanner{}, res[1].Doc)
	assert.Nil(t, res[1].PtrVal)
}

func TestMapRowsValuerRoundTrip(t *testing.T) {
	// the argument is encoded with Value(), the result is decoded with Scan()
	res, err := MapRows[mapperScannerModel](queryRows(t, `SELECT $1::text AS val`, mapperScanner{}))
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, mapperScanner{Src: "string:from-valuer", Calls: 1}, res[0].Val)
}

// withUTC normalizes the time location, pgx returns timestamptz in the local time zone.
func withUTC(m mapperAllTypes) mapperAllTypes {
	m.At = m.At.UTC()
	return m
}

func TestMapRowsUnexportedEmbedded(t *testing.T) {
	res, err := MapRows[typesShadow](queryRows(t, `SELECT 'outer' AS name, 'mail' AS email, 'middle' AS deep`))
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "outer", res[0].Name)
	assert.Equal(t, "mail", res[0].Email)
	assert.Equal(t, "middle", res[0].typesMiddle.Deep)
	assert.Empty(t, res[0].typesInner.Name)
	assert.Empty(t, res[0].typesInner.Deep)
}
