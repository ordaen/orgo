package cron

import (
	"context"
	"testing"
	"time"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupGroup removes the group from Global at the end of the test.
func cleanupGroup(t *testing.T, group string) {
	t.Cleanup(func() {
		stopJobs(t, func(j *Job) bool { return j.Type == group })
		RemoveGroup(group)
	})
}

// addLog adds a log with a message to the handler.
func addLog(t *testing.T, handler string) *CronLog {
	l, err := Logs.Create(&CronLog{Handler: handler, Status: "complete"})
	require.NoError(t, err)
	l.AddMessage("message")
	return l
}

func TestRegisterGroup(t *testing.T) {
	clearTables(t)
	cleanupGroup(t, "system")
	first, _ := signalJob("first", nil)
	second, _ := signalJob("second", nil)
	second.Type = "other"
	require.NoError(t, RegisterGroup("system", first, second))

	for _, id := range []string{"first", "second"} {
		assert.Equal(t, "system", Records.FindByHandler(id).Type, "the group is the type")
		assert.Equal(t, "system", Global.Job(id).Type)
		assert.True(t, Records.FindByHandler(id).Registered())
	}

	// a job removed from the group is stopped and its record is deleted with its logs
	addLog(t, "second")
	require.NoError(t, RegisterGroup("system", first))
	assert.Nil(t, Global.Job("second"))
	assert.False(t, Records.FindByHandler("second").ID.Valid())
	assert.Zero(t, Logs.CountWhere("handler = ?", "second"))
	assert.Zero(t, LogMessages.Count())
	assert.NotNil(t, Global.Job("first"))

	assert.EqualError(t, RegisterGroup("", first), "cron group can't be blank")
}

// TestRegisterGroupAgain checks a registered job gets its new definition and keeps its stored state.
func TestRegisterGroupAgain(t *testing.T) {
	clearTables(t)
	cleanupGroup(t, "system")
	job, oldRuns := signalJob("job", nil)
	require.NoError(t, RegisterGroup("system", job))
	rec := Records.FindByHandler("job")
	require.NoError(t, rec.UpdateSpec(everySecond))
	require.NoError(t, rec.Activate())
	receiveRun(t, oldRuns, 2*time.Second)
	old := Global.Job("job")

	renamed, newRuns := signalJob("job", nil)
	renamed.Name = "Renamed"
	renamed.Spec = "0 0 1 1 *"
	require.NoError(t, RegisterGroup("system", renamed))
	stored := Records.FindByHandler("job")
	assert.Equal(t, "Renamed", stored.Name, "the name is updated")
	assert.Equal(t, everySecond, stored.Spec, "the stored spec is kept")
	assert.True(t, stored.Active, "the active state is kept")

	// the new definition runs, the old schedule is stopped
	receiveRun(t, newRuns, 2*time.Second)
	waitStopped(t, old, oldRuns)
}

func TestRegisterGroupsSeparate(t *testing.T) {
	clearTables(t)
	cleanupGroup(t, "system")
	cleanupGroup(t, "billing")
	sys, _ := signalJob("sys", nil)
	bill, _ := signalJob("bill", nil)
	require.NoError(t, RegisterGroup("system", sys))
	require.NoError(t, RegisterGroup("billing", bill))

	// syncing a group does not touch the others
	require.NoError(t, RegisterGroup("system"))
	assert.Nil(t, Global.Job("sys"))
	assert.NotNil(t, Global.Job("bill"))
	assert.True(t, Records.FindByHandler("bill").ID.Valid())

	// a job ID is in one group
	dup, _ := signalJob("bill", nil)
	assert.EqualError(t, RegisterGroup("system", dup), "cron entry with ID 'bill' already added to 'billing'")
	assert.Equal(t, "billing", Records.FindByHandler("bill").Type)
}

// TestDisableGroup checks a disabled plugin keeps its records, and they are resumed when it is enabled again.
func TestDisableGroup(t *testing.T) {
	clearTables(t)
	cleanupGroup(t, "billing")
	job, runs := signalJob("invoice", nil)
	require.NoError(t, RegisterGroup("billing", job))
	rec := Records.FindByHandler("invoice")
	require.NoError(t, rec.Activate())
	receiveRun(t, runs, 2*time.Second)
	addLog(t, "invoice")
	registered := Global.Job("invoice")

	DisableGroup("billing")
	assert.Nil(t, Global.Job("invoice"))
	waitStopped(t, registered, runs)
	waitRecord(t, "invoice", func(r *CronRecord) bool { return !r.Running })
	rec = Records.FindByHandler("invoice")
	assert.True(t, rec.Active, "the record is kept active")
	assert.False(t, rec.Registered())
	assert.ErrorIs(t, rec.Run(), errCronJobNotFound)
	assert.Positive(t, Logs.CountWhere("handler = ?", "invoice"), "the logs are kept")

	// the startup of the other groups does not delete it
	require.NoError(t, RegisterGroup("system"))
	assert.True(t, Records.FindByHandler("invoice").ID.Valid())

	require.NoError(t, RegisterGroup("billing", job))
	receiveRun(t, runs, 2*time.Second)
}

func TestRemoveGroup(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("invoice", nil)
	require.NoError(t, RegisterGroup("billing", job))
	addLog(t, "invoice")
	// a record of the group whose job is not registered, like of a disabled plugin
	_, err := Records.Create(&CronRecord{Handler: "old", Name: "old", Type: "billing"})
	require.NoError(t, err)
	addLog(t, "old")
	other, _ := signalJob("other", nil)
	require.NoError(t, RegisterGroup("system", other))
	cleanupGroup(t, "system")

	require.NoError(t, RemoveGroup("billing"))
	assert.Nil(t, Global.Job("invoice"))
	assert.Zero(t, Records.CountWhere("type = ?", "billing"))
	assert.Zero(t, Logs.CountWhere("handler IN ('invoice', 'old')"))
	assert.Zero(t, LogMessages.Count())
	assert.True(t, Records.FindByHandler("other").ID.Valid())
	assert.EqualError(t, RemoveGroup(""), "cron group can't be blank")
}

func TestUnregisterDeletesLogs(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("single", nil)
	require.NoError(t, Global.Register(job))
	addLog(t, "single")
	require.NoError(t, Global.Unregister("single"))
	assert.Zero(t, Records.Count())
	assert.Zero(t, Logs.Count())
	assert.Zero(t, LogMessages.Count())
}

// TestPluginToTypeMigration checks the schema moves the group from the plugin column to the type.
func TestPluginToTypeMigration(t *testing.T) {
	clearTables(t)
	ctx := context.Background()
	_, err := pg.DB.Exec(ctx, `INSERT INTO cron_records (handler, name, type, plugin) VALUES
		('a', 'a', 'system', NULL), ('b', 'b', 'test', 'billing'), ('c', 'c', '', '')`)
	require.NoError(t, err)
	_, err = pg.DB.Exec(ctx, SQLSchemaRecords)
	require.NoError(t, err)
	assert.Equal(t, "system", Records.FindByHandler("a").Type)
	assert.Equal(t, "billing", Records.FindByHandler("b").Type)
	assert.Empty(t, Records.FindByHandler("c").Type)
	n, err := pg.CountWhere("cron_records", "plugin IS NOT NULL AND plugin <> ''")
	require.NoError(t, err)
	assert.Zero(t, n)
}
