package handlers

import (
	"testing"

	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type jsonInner struct {
	A string `json:"a"`
	B string `json:"b"`
}

type jsonItem struct {
	model.Base[model.ID]
	Name    string            `json:"name"`
	Total   int               `json:"total" api:"readonly"`
	Kind    string            `json:"kind" api:"createonly"`
	Tags    []string          `json:"tags"`
	Meta    map[string]string `json:"meta"`
	Count   uint64            `json:"count,string"`
	Note    string            `json:"-"`
	Inner   jsonInner         `json:"inner"`
	OwnerID model.ID          `json:"owner_id"`
	Virtual string            `json:"virtual" db:"-"`
}

func (*jsonItem) TableName() string     { return "json_items" }
func (*jsonItem) ExtraFields() []string { return []string{"label", "none"} }

func (m *jsonItem) ExtraField(name string) any {
	if name == "label" {
		return "L-" + m.Name
	}
	return nil
}

func TestDecodeRecordUpdate(t *testing.T) {
	tags := []string{"a", "b"}
	meta := map[string]string{"k": "v"}
	rec := &jsonItem{Name: "old", Total: 1, Kind: "k1", Tags: tags, Meta: meta, Inner: jsonInner{A: "a", B: "b"}}
	rec.ID = 7

	ch, cols, err := DecodeRecord([]byte(`{
		"id": "99", "created": "2020-01-01T00:00:00Z", "name": "new", "total": 5, "kind": "k2",
		"tags": ["c"], "meta": {"x": "y"}, "count": "12", "Note": "n", "inner": {"a": "z"},
		"owner_id": "3", "virtual": "v", "unknown": 1}`), rec, false)
	require.NoError(t, err)

	assert.Equal(t, model.ID(7), rec.ID)
	assert.True(t, rec.Created.IsZero())
	assert.Equal(t, "new", rec.Name)
	assert.Equal(t, 1, rec.Total, "readonly")
	assert.Equal(t, "k1", rec.Kind, "createonly")
	assert.Equal(t, []string{"c"}, rec.Tags)
	assert.Equal(t, []string{"a", "b"}, tags, "the old slice is not modified")
	assert.Equal(t, map[string]string{"x": "y"}, rec.Meta)
	assert.Equal(t, map[string]string{"k": "v"}, meta, "the old map is not modified")
	assert.Equal(t, uint64(12), rec.Count)
	assert.Empty(t, rec.Note)
	assert.Equal(t, jsonInner{A: "z"}, rec.Inner, "an object replaces the struct")
	assert.Equal(t, model.ID(3), rec.OwnerID)
	assert.Equal(t, "v", rec.Virtual)

	assert.Equal(t, []string{"name", "tags", "meta", "count", "inner", "owner_id"}, cols)
	assert.Equal(t, changes.Changes{
		{Key: "name", From: "old", To: "new"},
		{Key: "tags", From: "[\n    \"a\",\n    \"b\"\n  ]", To: "[\n    \"c\"\n  ]"},
		{Key: "meta.k", From: "v", To: ""},
		{Key: "meta.x", From: "", To: "y"},
		{Key: "count", From: "0", To: "12"},
		{Key: "inner.B", From: "b", To: ""},
		{Key: "inner.A", From: "a", To: "z"},
		{Key: "owner_id", From: "0", To: "3"},
		{Key: "virtual", From: "", To: "v"},
	}, sortedByKeyOrder(ch, "name", "tags", "meta.k", "meta.x", "count", "inner.B", "inner.A", "owner_id", "virtual"))
}

// sortedByKeyOrder returns the changes in the order of the keys, so the test does not depend on the field order.
func sortedByKeyOrder(ch changes.Changes, keys ...string) changes.Changes {
	res := make(changes.Changes, 0, len(ch))
	for _, k := range keys {
		for _, c := range ch {
			if c.Key == k {
				res = append(res, c)
			}
		}
	}
	if len(res) != len(ch) {
		return ch
	}
	return res
}

func TestDecodeRecordUnchanged(t *testing.T) {
	rec := &jsonItem{Name: "same", Tags: []string{"a"}}
	ch, cols, err := DecodeRecord([]byte(`{"name": "same", "tags": ["a"]}`), rec, false)
	require.NoError(t, err)
	assert.Empty(t, ch)
	assert.Empty(t, cols)
}

func TestDecodeRecordCreate(t *testing.T) {
	rec := &jsonItem{}
	_, cols, err := DecodeRecord([]byte(`{"name": "n", "kind": "k", "total": 3}`), rec, true)
	require.NoError(t, err)
	assert.Equal(t, "k", rec.Kind, "createonly is set on create")
	assert.Equal(t, 0, rec.Total, "readonly")
	assert.Equal(t, []string{"name", "kind"}, cols)
}

func TestDecodeRecordErrors(t *testing.T) {
	for body, want := range map[string]Error{
		``:                    ErrorBadRequest("the request body is not a JSON object"),
		`null`:                ErrorBadRequest("the request body is not a JSON object"),
		`[1]`:                 ErrorBadRequest("the request body is not a JSON object"),
		`{"name": 1}`:         ErrorInvalidAttribute("name", "name must be a string"),
		`{"tags": "x"}`:       ErrorInvalidAttribute("tags", "tags must be an array"),
		`{"count": 5}`:        ErrorInvalidAttribute("count", "count must be a string"),
		`{"count": "x"}`:      ErrorInvalidAttribute("count", "count is invalid: invalid character 'x' looking for beginning of value"),
		`{"owner_id": "abc"}`: ErrorInvalidAttribute("owner_id", "owner_id is invalid: invalid ID format"),
	} {
		_, _, err := DecodeRecord([]byte(body), &jsonItem{}, true)
		assert.Equal(t, want, err, body)
	}
	_, _, err := DecodeRecord([]byte(`{}`), (*jsonItem)(nil), true)
	assert.Error(t, err)
}

func TestEncodeData(t *testing.T) {
	rec := &jsonItem{Name: "n", Total: 2}
	rec.ID = 5

	b, err := EncodeData(rec, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"5","created":"0001-01-01T00:00:00Z","updated":"0001-01-01T00:00:00Z","name":"n",
		"total":2,"kind":"","tags":null,"meta":null,"count":"0","inner":{"a":"","b":""},"owner_id":"0",
		"virtual":"","label":"L-n"}`, string(b))

	b, err = EncodeData(rec, []string{"name", "label", "none"})
	require.NoError(t, err)
	assert.Equal(t, `{"id":"5","name":"n","label":"L-n"}`, string(b), "the id, the fields and the extra fields in order")

	b, err = EncodeData([]*jsonItem{rec, {Name: "m"}}, []string{"name"})
	require.NoError(t, err)
	assert.Equal(t, `[{"id":"5","name":"n"},{"name":"m"}]`, string(b))

	b, err = EncodeData([]jsonItem{*rec}, []string{"label"})
	require.NoError(t, err)
	assert.Equal(t, `[{"id":"5","label":"L-n"}]`, string(b), "the values of a slice use the pointer methods")

	b, err = EncodeData(&[]*jsonItem{rec}, []string{"name"})
	require.NoError(t, err)
	assert.Equal(t, `[{"id":"5","name":"n"}]`, string(b))
}

func TestEncodeDataPlain(t *testing.T) {
	for _, tc := range []struct {
		data   any
		fields []string
		want   string
	}{
		{nil, nil, `null`},
		{true, []string{"x"}, `true`},
		{[]byte("hi"), []string{"x"}, `"aGk="`},
		{[]int{1, 2}, []string{"x"}, `[1,2]`},
		{[]*jsonItem(nil), []string{"x"}, `null`},
		{(*jsonItem)(nil), nil, `null`},
		{map[string]int{"id": 1, "a": 2, "b": 3}, []string{"b"}, `{"b":3,"id":1}`},
		{[]map[string]int{{"a": 1}}, nil, `[{"a":1}]`},
	} {
		b, err := EncodeData(tc.data, tc.fields)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(b))
	}
}
