package cron

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCronRecordCanUpdateSpec(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("spec", nil)
	job.Options.NoSpecChange = true
	rec := registerJob(t, job)
	require.EqualError(t, rec.canUpdateSpec("1 * * * *"), "no spec change allowed for this cron job")
	require.EqualError(t, rec.UpdateSpec("1 * * * *"), "no spec change allowed for this cron job")

	Global.Job("spec").Options.NoSpecChange = false
	require.NoError(t, rec.canUpdateSpec("1 * * * *"))
	require.EqualError(t, rec.canUpdateSpec("*"), "missing field(s)")
	require.EqualError(t, rec.canUpdateSpec(""), "missing field(s)")
	require.EqualError(t, rec.canUpdateSpec("100 * * * *"), "syntax error in minute field: '100'")
}

func TestActivateDeactivate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clearTables(t)
		job, runs := signalJob("schedule", nil)
		rec := registerJob(t, job)
		assertNotScheduled(t, Global.Job("schedule"))

		require.NoError(t, rec.Activate())
		assert.True(t, rec.Active)
		stored := Records.FindByHandler("schedule")
		assert.True(t, stored.Active)
		assert.WithinDuration(t, time.Now(), stored.NextRun, 2*time.Second)
		receiveRun(t, runs, 2*time.Second)
		receiveRun(t, runs, 2*time.Second)
		waitRecord(t, "schedule", func(r *CronRecord) bool { return !r.LastRun.IsZero() && !r.NextRun.IsZero() })

		require.NoError(t, rec.Deactivate())
		assert.False(t, rec.Active)
		waitStopped(t, Global.Job("schedule"), runs)
		stored = Records.FindByHandler("schedule")
		assert.False(t, stored.Active)
		assert.True(t, stored.NextRun.IsZero())
	})
}

// TestDeactivateDuringRun checks Deactivate does not wait for the running job, waiting would deadlock
// the bubble, and the finished run does not set the next run of the inactive job.
func TestDeactivateDuringRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clearTables(t)
		release := make(chan struct{})
		job, runs := signalJob("busy", release)
		rec := registerJob(t, job)
		require.NoError(t, rec.Activate())
		receiveRun(t, runs, 2*time.Second)

		require.NoError(t, rec.Deactivate())
		close(release)

		stored := waitRecord(t, "busy", func(r *CronRecord) bool { return !r.Running })
		assert.False(t, stored.Active)
		assert.True(t, stored.NextRun.IsZero())
		waitStopped(t, Global.Job("busy"), runs)
	})
}

func TestActivateCanEnable(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("guarded", nil)
	job.CanEnable = func() error { return assert.AnError }
	rec := registerJob(t, job)
	assert.ErrorIs(t, rec.Activate(), assert.AnError)
	assert.False(t, Records.FindByHandler("guarded").Active)
}

func TestActivateInvalidSpec(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("invalid", nil)
	rec := registerJob(t, job)
	rec.Spec = "*"
	assert.EqualError(t, rec.Activate(), "invalid spec format")
}

func TestUpdateSpec(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clearTables(t)
		job, runs := signalJob("respec", nil)
		job.Spec = "0 0 1 1 *"
		rec := registerJob(t, job)
		require.NoError(t, rec.Activate())
		next := Global.Job("respec").Next(time.Now())
		assert.Equal(t, []any{time.January, 1}, []any{next.Month(), next.Day()}, "the job is scheduled by its spec")

		require.NoError(t, rec.UpdateSpec(everySecond))
		assert.Equal(t, everySecond, Records.FindByHandler("respec").Spec)
		receiveRun(t, runs, 2*time.Second)
	})
}

func TestUpdateSpecInactive(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("respec-inactive", nil)
	rec := registerJob(t, job)
	require.NoError(t, rec.UpdateSpec("0 0 * * *"))
	stored := Records.FindByHandler("respec-inactive")
	assert.Equal(t, "0 0 * * *", stored.Spec)
	assert.False(t, stored.Active)
	assertNotScheduled(t, Global.Job("respec-inactive"))
}
