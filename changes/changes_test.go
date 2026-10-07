package changes

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ordaen/orgo/crypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeJSON(t *testing.T) {
	b, err := json.Marshal(Change{Key: "key1", From: "from", To: "to"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"key1","from":"from","to":"to"}`, string(b))

	var ch Changes
	require.NoError(t, json.Unmarshal([]byte(`[{"key":"name","from":"a","to":"b"}, ["Active", "true", "false"]]`), &ch))
	assert.Equal(t, Changes{{"name", "a", "b"}, {"Active", "true", "false"}}, ch)

	var c Change
	assert.Error(t, json.Unmarshal([]byte(`[1, 2, 3]`), &c))
	assert.Error(t, json.Unmarshal([]byte(`"key"`), &c))
}

func TestChangesAdd(t *testing.T) {
	ch := Changes{}
	ch.Add("key1", "from", "to")
	assert.Equal(t, Changes{Change{"key1", "from", "to"}}, ch)
	ch.Add("key1", "from", "to")
	assert.Equal(t, Changes{Change{"key1", "from", "to"}}, ch)
	ch.Add("key2", "from2", "to2")
	assert.Equal(t, Changes{Change{"key1", "from", "to"}, Change{"key2", "from2", "to2"}}, ch)
}

func TestDiff(t *testing.T) {
	var slice []string

	assert.Empty(t, ValuesDiff(reflect.ValueOf(slice), reflect.ValueOf([]string{}), "field"))
	b, _ := json.MarshalIndent([]string{"1", "2"}, "  ", "  ")
	assert.Equal(t, Changes{Change{"field", "[]", string(b)}}, ValuesDiff(reflect.ValueOf(slice), reflect.ValueOf([]string{"1", "2"}), "field"))
	b1, _ := json.MarshalIndent([]string{"2", "3"}, "  ", "  ")
	assert.Equal(t, Changes{Change{"field", string(b), string(b1)}}, ValuesDiff(reflect.ValueOf([]string{"1", "2"}), reflect.ValueOf([]string{"2", "3"}), "field"))
	assert.Equal(t, Changes{Change{"field", string(b), "[]"}}, ValuesDiff(reflect.ValueOf([]string{"1", "2"}), reflect.ValueOf(slice), "field"))
	assert.Equal(t, Changes{Change{"field", string(b), "[]"}}, ValuesDiff(reflect.ValueOf([]string{"1", "2"}), reflect.ValueOf([]string{}), "field"))
}

func TestDiffMask(t *testing.T) {
	p := crypt.NewString("test")
	p1 := crypt.NewString("test2")

	assert.Empty(t, ValuesDiff(reflect.ValueOf(p), reflect.ValueOf(p), "field"))
	assert.Equal(t, Changes{Change{"field", string(p), string(p1)}}, ValuesDiff(reflect.ValueOf(p), reflect.ValueOf(p1), "field"))
	MaskChangesFor[crypt.String]("***")
	assert.Empty(t, ValuesDiff(reflect.ValueOf(p), reflect.ValueOf(p), "field"))
	assert.Equal(t, Changes{Change{"field", "***", "***"}}, ValuesDiff(reflect.ValueOf(p), reflect.ValueOf(p1), "field"))
	UnmaskChangesFor[crypt.String]()
	assert.Empty(t, ValuesDiff(reflect.ValueOf(p), reflect.ValueOf(p), "field"))
	assert.Equal(t, Changes{Change{"field", string(p), string(p1)}}, ValuesDiff(reflect.ValueOf(p), reflect.ValueOf(p1), "field"))
}

type diffInner struct {
	Name string
}

type diffBase struct {
	ID int
}

type diffRecord struct {
	diffBase
	Inner   *diffInner
	Tags    map[int]string
	At      time.Time
	Skipped string `db:"-"`
	private string
}

func TestDiffStructs(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	r1 := diffRecord{diffBase: diffBase{ID: 1}, At: at, Skipped: "a", private: "a"}
	r2 := diffRecord{
		diffBase: diffBase{ID: 2},
		Inner:    &diffInner{Name: "n"},
		Tags:     map[int]string{2: "b", 1: "a"},
		At:       at.Add(time.Second),
		Skipped:  "b",
		private:  "b",
	}

	assert.Equal(t, Changes{
		{"r.ID", "1", "2"},
		{"r.Inner.Name", "", "n"},
		{"r.Tags.1", "", "a"},
		{"r.Tags.2", "", "b"},
		{"r.At", "2026-10-01T12:00:00Z", "2026-10-01T12:00:01Z"},
	}, ValuesDiff(reflect.ValueOf(r1), reflect.ValueOf(&r2), "r"))

	assert.Equal(t, Changes{
		{"r.Inner.Name", "n", ""},
	}, ValuesDiff(reflect.ValueOf(diffRecord{Inner: &diffInner{Name: "n"}}), reflect.ValueOf(diffRecord{}), "r"))
}

func TestDiffTimeMonotonic(t *testing.T) {
	now := time.Now()
	assert.Empty(t, ValuesDiff(reflect.ValueOf(now), reflect.ValueOf(now.Round(0)), "at"))
}

func TestDiffMaskStruct(t *testing.T) {
	MaskChangesFor[*diffInner]("***")
	t.Cleanup(UnmaskChangesFor[diffInner])

	assert.Empty(t, ValuesDiff(reflect.ValueOf(&diffInner{Name: "a"}), reflect.ValueOf(diffInner{Name: "a"}), "f"))
	assert.Equal(t, Changes{{"f", "***", "***"}}, ValuesDiff(reflect.ValueOf(diffInner{Name: "a"}), reflect.ValueOf(diffInner{Name: "b"}), "f"))
	assert.Equal(t, Changes{{"f", "***", "***"}}, ValuesDiff(reflect.ValueOf((*diffInner)(nil)), reflect.ValueOf(&diffInner{Name: "b"}), "f"))
}

func TestStringify(t *testing.T) {
	assert.Equal(t, "", Stringify(nil))
	assert.Equal(t, "", Stringify((*int)(nil)))
	n := int8(-3)
	assert.Equal(t, "-3", Stringify(&n))
	assert.Equal(t, "true", Stringify(true))
	assert.Equal(t, "[]", Stringify([]int(nil)))
	assert.Equal(t, "1.5", Stringify(1.5))
}

func TestChangesContainsGet(t *testing.T) {
	ch := Changes{{"a", "1", "2"}}
	assert.True(t, ch.Contains("b", "a"))
	assert.False(t, ch.Contains("b"))
	from, to := ch.Get("a")
	assert.Equal(t, "1", from)
	assert.Equal(t, "2", to)
}

func TestKeys(t *testing.T) {
	c := Changes{{Key: "b"}, {Key: "a"}, {Key: "b"}}
	assert.Equal(t, []string{"b", "a"}, c.Keys())
	assert.Empty(t, Changes(nil).Keys())
}
