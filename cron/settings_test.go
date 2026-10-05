package cron

import (
	"encoding/json"
	"testing"

	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettings(t *testing.T) {
	assert.Equal(t, 30, keepLogRecords())
	require.NoError(t, settings.Settings.Update("cron", json.RawMessage(`{"keep_log_records":50}`)))
	t.Cleanup(func() {
		settings.Settings.Update("cron", json.RawMessage(`{"keep_log_records":30}`))
	})
	assert.Equal(t, 50, keepLogRecords())
	assert.Equal(t, 50, Settings.KeepLogRecords)
}

// TestReconnect checks the cron settings do not fail a second connect.
func TestReconnect(t *testing.T) {
	pg.Close()
	require.NoError(t, pg.Connect(testConfig()))
	assert.Equal(t, 30, keepLogRecords())
}
