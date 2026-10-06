package settings

import (
	"testing"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelSettingsRecord(t *testing.T) {
	require.NoError(t, pg.ClearTables("settings"))
	s := &TestSettings{Name: t.Name(), Slice: []string{"a", "b"}}
	r, err := newRecord("test-record", s)
	assert.NoError(t, err)
	assert.Equal(t, "settings", r.TableName())
	assert.Equal(t, "Setting", pg.ModelType(r))
	_, err = pg.CreateModel(r)
	require.NoError(t, err)
}
