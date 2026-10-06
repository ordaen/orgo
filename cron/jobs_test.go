package cron

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobsRegister(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("first", nil)
	rec := registerJob(t, job)
	assert.Equal(t, everySecond, rec.Spec)
	assert.Equal(t, "first", rec.Name)
	assert.Equal(t, "test", rec.Type)
	assert.False(t, rec.Active)
	assert.False(t, rec.Running)
	assert.Same(t, Global.Job("first"), Global.Job("first"))
	assert.Nil(t, Global.Job("missing"))

	require.EqualError(t, Global.Register(job), "cron entry with ID 'first' already added")
	assert.Equal(t, 1, Records.Count())

	require.NoError(t, Global.Unregister("first"))
	assert.Zero(t, Records.Count())
	assert.Nil(t, Global.Job("first"))
	require.NoError(t, Global.Unregister("first"), "a missing job is not an error")
}

func TestJobsRegisterActive(t *testing.T) {
	clearTables(t)
	job, runs := signalJob("active", nil)
	job.Active = true
	rec := registerJob(t, job)
	assert.True(t, rec.Active)
	assert.False(t, rec.NextRun.IsZero())
	receiveRun(t, runs, 2*time.Second)
}

// TestJobsRegisterExisting checks the stored record wins over the job: an active record is activated,
// and a record left running by a stopped process is not running anymore.
func TestJobsRegisterExisting(t *testing.T) {
	clearTables(t)
	_, err := Records.Create(&CronRecord{Handler: "existing", Name: "existing", Spec: everySecond, Active: true, Running: true, LogID: 9})
	require.NoError(t, err)
	job, runs := signalJob("existing", nil)
	job.Spec = "0 0 * * *"
	rec := registerJob(t, job)
	assert.False(t, rec.Running)
	assert.False(t, rec.LogID.Valid())
	assert.Equal(t, everySecond, rec.Spec, "the stored spec is used")
	receiveRun(t, runs, 2*time.Second)
}

func TestJobsStop(t *testing.T) {
	clearTables(t)
	job, runs := signalJob("stopped", nil)
	job.Active = true
	registerJob(t, job)
	receiveRun(t, runs, 2*time.Second)

	Global.Stop()
	waitStopped(t, Global.Job("stopped"), runs)
	assert.True(t, Records.FindByHandler("stopped").Active, "the record is not changed")
}

func TestJobsNext(t *testing.T) {
	next, err := Global.Next("0 0 * * *")
	require.NoError(t, err)
	assert.True(t, next.After(time.Now()))
	_, err = Global.Next("bad")
	assert.Error(t, err)
}
