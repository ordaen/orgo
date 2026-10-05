package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestSettings struct {
	Name  string   `json:"name,omitempty"`
	Slice []string `json:"slice,omitempty"`
}

func TestStoreInitialization(t *testing.T) {
	assert.NotNil(t, Settings.types)
}

func TestSettingsRegister(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	ts1 := TestSettings{Name: "name"}
	wts := "str"
	assert.EqualError(t, Register("a1", wts), "SETTINGS REGISTER [a1]: expect pointer to data structure got 'string'")
	assert.EqualError(t, Register("a1", ts1), "SETTINGS REGISTER [a1]: expect pointer to data structure got 'struct'")
	assert.EqualError(t, Register("a1", &wts), "SETTINGS REGISTER [a1]: expect pointer to data structure got pointer to 'string'")
	assert.NoError(t, Register("a1", &ts1))
	assert.EqualError(t, Register("a1", &ts1), "SETTINGS REGISTER [a1]: duplicate key previously assigned to '*settings.TestSettings'")
	assert.NoError(t, Register("a2", &ts1))
	Settings.Clear()
}

func TestSettingsUpdate(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	ts := TestSettings{Name: "name"}
	assert.NoError(t, Register("a1", &ts))
	assert.EqualError(t, Settings.Update("a0", []byte(`{"name":1,"slice":["1","2"]}`)), "SETTINGS UPDATE [a0]: key not registered")
	assert.EqualError(t, Settings.Update("a1", []byte(`{"name":1,"slice":["1","2"]}`)), "SETTINGS UPDATE [a1]: json: cannot unmarshal number into Go struct field TestSettings.name of type string")
	assert.NoError(t, Settings.Update("a1", []byte(`{"name":"name1","slice":["1","2"]}`)))
	assert.Equal(t, "name1", ts.Name)
	assert.Equal(t, []string{"1", "2"}, ts.Slice)
	r := FindByKey("a1")
	assert.Equal(t, `{"name": "name1", "slice": ["1", "2"]}`, string(r.Data))
	Settings.Clear()
}

func TestSettingsUpdateRecords(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))

	ts := TestSettings{Name: "name"}
	assert.NoError(t, Register("a1", &ts))
	count, err := pg.CountWhere("settings", "type = ?", "a1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	assert.NoError(t, Settings.Update("a1", []byte(`{"name":"name1","slice":["1","2"]}`)))
	r := FindByKey("a1")
	assert.Equal(t, `{"name": "name1", "slice": ["1", "2"]}`, string(r.Data))
	count, err = pg.CountWhere("settings", "type = ?", "a1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, `{"name": "name1", "slice": ["1", "2"]}`, string(r.Data))
	assert.True(t, r.ID.Valid())

	assert.NoError(t, Settings.Update("a1", []byte(`{"name":"name2","slice":["1","2"]}`)))
	r = FindByKey("a1")
	assert.Equal(t, `{"name": "name2", "slice": ["1", "2"]}`, string(r.Data))
	count, err = pg.CountWhere("settings", "type = ?", "a1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, "name2", ts.Name)
	assert.Equal(t, []string{"1", "2"}, ts.Slice)
	Settings.Clear()
}

func TestSettingsGetData(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	ts := TestSettings{Name: "some name"}
	assert.NoError(t, Register("a1", &ts))
	assert.Nil(t, Settings.GetData("a0"))
	assert.Equal(t, &ts, Settings.GetData("a1"))
	assert.NoError(t, Settings.Update("a1", []byte(`{"name":"name1","slice":["1","2"]}`)))
	assert.Equal(t, &TestSettings{Name: "name1", Slice: []string{"1", "2"}}, Settings.GetData("a1"))
	Settings.Clear()
}

func TestSettingsRegisterWithNoRecordsInDB(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	ts := TestSettings{Name: "some name"}
	ts2 := TestSettings{Name: "some name 2"}
	assert.NoError(t, Register("a1", &ts))
	assert.NoError(t, Register("a2", &ts2))
	assert.Equal(t, "some name", ts.Name)
	assert.Equal(t, "some name 2", ts2.Name)
	Settings.Clear()
}

func TestSettingsRegisterWithRecordsInDB(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	r1 := &Setting{Type: "a1", Data: json.RawMessage(`{"name":"a1 name"}`)}
	r2 := &Setting{Type: "a2", Data: json.RawMessage(`{"name":"a2 name"}`)}
	_, err := pg.CreateModel(r1)
	require.NoError(t, err)
	_, err = pg.CreateModel(r2)
	require.NoError(t, err)
	ts := TestSettings{Name: "some name"}
	ts2 := TestSettings{Name: "some name 2"}
	assert.NoError(t, Register("a1", &ts))
	assert.NoError(t, Register("a2", &ts2))
	assert.Equal(t, "a1 name", ts.Name)
	assert.Equal(t, "a2 name", ts2.Name)
	Settings.Clear()
}

// validatedSettings rejects the name "bad" and has fields not encoded in JSON.
type validatedSettings struct {
	Name     string            `json:"name"`
	Slice    []string          `json:"slice"`
	Map      map[string]string `json:"map"`
	Secret   string            `json:"-"`
	internal int
}

func (v *validatedSettings) BeforeUpdate() error {
	if v.Name == "bad" {
		return errors.New("bad name")
	}
	return nil
}

// TestSettingsUpdateRejected checks a rejected update changes neither the settings nor the table.
func TestSettingsUpdateRejected(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	slice := make([]string, 2, 4)
	slice[0], slice[1] = "a", "b"
	vs := &validatedSettings{Name: "good", Slice: slice, Map: map[string]string{"k": "v"}}
	require.NoError(t, Register("v", vs))

	err := Settings.Update("v", []byte(`{"name":"bad","slice":["x","y"],"map":{"k":"x"}}`))
	assert.EqualError(t, err, "SETTINGS UPDATE [v]: bad name")
	assert.Equal(t, "good", vs.Name)
	assert.Equal(t, []string{"a", "b"}, slice, "the slice of the settings is not changed")
	assert.Equal(t, map[string]string{"k": "v"}, vs.Map, "the map of the settings is not changed")
	count, err := pg.CountWhere("settings", "type = ?", "v")
	require.NoError(t, err)
	assert.Zero(t, count)
}

// TestSettingsUpdateKeepsFields checks the fields not in the update data and not encoded in JSON keep their values.
func TestSettingsUpdateKeepsFields(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	vs := &validatedSettings{Name: "good", Slice: []string{"a"}, Secret: "secret", internal: 5}
	require.NoError(t, Register("v", vs))
	held := vs.Slice

	require.NoError(t, Settings.Update("v", []byte(`{"slice":["x","y"]}`)))
	assert.Equal(t, "good", vs.Name, "missing in the data")
	assert.Equal(t, []string{"x", "y"}, vs.Slice)
	assert.Equal(t, []string{"a"}, held, "a held slice is not changed")
	assert.Equal(t, "secret", vs.Secret)
	assert.Equal(t, 5, vs.internal)
	assert.Equal(t, `{"map": null, "name": "good", "slice": ["x", "y"]}`, string(FindByKey("v").Data))
}

type embeddedBase struct {
	Level int `json:"level"`
}

type embeddedSettings struct {
	embeddedBase
	Name string `json:"name"`
}

func TestSettingsUpdateEmbedded(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	es := &embeddedSettings{Name: "n"}
	require.NoError(t, Register("e", es))
	require.NoError(t, Settings.Update("e", []byte(`{"level":3}`)))
	assert.Equal(t, 3, es.Level)
	assert.Equal(t, "n", es.Name)
}

// TestSettingsRegisterBeforeConnect checks the settings registered before connect are loaded on connect.
func TestSettingsRegisterBeforeConnect(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	_, err := pg.CreateModel(&Setting{Type: "early", Data: json.RawMessage(`{"name":"stored"}`)})
	require.NoError(t, err)

	pg.Close()
	ts := &TestSettings{Name: "default"}
	require.NoError(t, Register("early", ts))
	assert.Equal(t, "default", ts.Name)
	require.NoError(t, pg.Connect(testConfig()))
	assert.Equal(t, "stored", ts.Name)
}

func TestSettingsRegisterInvalidStored(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	_, err := pg.CreateModel(&Setting{Type: "inv", Data: json.RawMessage(`{"name":1}`)})
	require.NoError(t, err)
	ts := &TestSettings{Name: "default"}
	assert.ErrorContains(t, Register("inv", ts), "SETTINGS LOAD [inv]")
	assert.Equal(t, "default", ts.Name)
}

func TestSettingsGet(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	ts := &TestSettings{Name: "name"}
	require.NoError(t, Register("g", ts))
	got, ok := Get[TestSettings]("g")
	assert.True(t, ok)
	assert.Equal(t, "name", got.Name)
	_, ok = Get[validatedSettings]("g")
	assert.False(t, ok, "another type")
	_, ok = Get[TestSettings]("missing")
	assert.False(t, ok)
}

func TestSettingsGetDatas(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	ts := &TestSettings{}
	require.NoError(t, Register("d", ts))
	all := Settings.GetDatas()
	assert.Equal(t, map[string]any{"d": ts}, all)
	delete(all, "d")
	assert.NotNil(t, Settings.GetData("d"), "GetDatas returns a copy")
}

func TestSettingsRegisterConcurrent(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 8 {
		wg.Go(func() {
			if Register("same", &TestSettings{}) == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), ok.Load(), "only one registration of a key succeeds")
}

func TestSettingsUpdateConcurrent(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	t.Cleanup(func() { Settings.Clear() })
	ts := &TestSettings{Name: "start"}
	require.NoError(t, Register("c", ts))
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for j := range 5 {
				assert.NoError(t, Settings.Update("c", []byte(fmt.Sprintf(`{"name":"n%d-%d","slice":["a"]}`, i, j))))
			}
		})
		wg.Go(func() {
			for range 50 {
				got, _ := Get[TestSettings]("c")
				_ = len(got.Name) + len(got.Slice)
				Settings.RLock()
				_ = ts.Name
				Settings.RUnlock()
			}
		})
	}
	wg.Wait()
	count, err := pg.CountWhere("settings", "type = ?", "c")
	require.NoError(t, err)
	assert.Equal(t, 1, count, "one row per key")
	assert.Equal(t, ts.Name, storedName(t, FindByKey("c").Data), "the table has the last update")
}

func storedName(t *testing.T, data []byte) string {
	var v TestSettings
	require.NoError(t, json.Unmarshal(data, &v))
	return v.Name
}
