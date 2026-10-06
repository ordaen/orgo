package cron

import (
	"encoding/json"
	"testing"

	"github.com/ordaen/orgo/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveOldRecords(t *testing.T) {
	clearTables(t)
	require.NoError(t, settings.Settings.Update("cron", json.RawMessage(`{"keep_log_records":2}`)))
	t.Cleanup(func() { settings.Settings.Update("cron", json.RawMessage(`{"keep_log_records":30}`)) })

	var created []*CronLog
	for i := range 5 {
		// the logs are created directly, the cleanup is run below
		l, err := Logs.Create(&CronLog{Handler: "h", Status: "complete", Protected: i == 0})
		require.NoError(t, err)
		l.AddMessage("message %d", i)
		created = append(created, l)
	}
	other, err := Logs.Create(&CronLog{Handler: "other", Status: "complete"})
	require.NoError(t, err)
	other.AddMessage("other")

	require.NoError(t, removeOldRecords("h"))
	var kept []int
	for i, l := range created {
		if Logs.FindByID(l.ID).ID.Valid() {
			kept = append(kept, i)
		}
	}
	assert.Equal(t, []int{0, 3, 4}, kept, "the protected log and the newest ones are kept")
	assert.Equal(t, 4, LogMessages.Count(), "the messages of the deleted logs are deleted")
	assert.True(t, Logs.FindByID(other.ID).ID.Valid(), "the logs of other handlers are kept")
}

func TestRemoveOldRecordsKeepAll(t *testing.T) {
	clearTables(t)
	require.NoError(t, settings.Settings.Update("cron", json.RawMessage(`{"keep_log_records":0}`)))
	t.Cleanup(func() { settings.Settings.Update("cron", json.RawMessage(`{"keep_log_records":30}`)) })
	for range 3 {
		_, err := Logs.Create(&CronLog{Handler: "h", Status: "complete"})
		require.NoError(t, err)
	}
	require.NoError(t, removeOldRecords("h"))
	assert.Equal(t, 3, Logs.Count())
}
