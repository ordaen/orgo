package cron

import (
	"testing"
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
	clearTables(t)
	job, runs := signalJob("schedule", nil)
	rec := registerJob(t, job)
	assertNoRun(t, runs, 1200*time.Millisecond)

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
	waitRecord(t, "schedule", func(r *CronRecord) bool { return !r.Running })
	for len(runs) > 0 {
		<-runs
	}
	assertNoRun(t, runs, 1500*time.Millisecond)
	stored = Records.FindByHandler("schedule")
	assert.False(t, stored.Active)
	assert.True(t, stored.NextRun.IsZero())
}

// TestDeactivateDuringRun checks Deactivate does not wait for the running job, and the finished run
// does not set the next run of the inactive job.
func TestDeactivateDuringRun(t *testing.T) {
	clearTables(t)
	release := make(chan struct{})
	job, runs := signalJob("busy", release)
	rec := registerJob(t, job)
	require.NoError(t, rec.Activate())
	receiveRun(t, runs, 2*time.Second)

	start := time.Now()
	require.NoError(t, rec.Deactivate())
	assert.Less(t, time.Since(start), 500*time.Millisecond)
	close(release)

	stored := waitRecord(t, "busy", func(r *CronRecord) bool { return !r.Running })
	assert.False(t, stored.Active)
	assert.True(t, stored.NextRun.IsZero())
	assertNoRun(t, runs, 1500*time.Millisecond)
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
	clearTables(t)
	job, runs := signalJob("respec", nil)
	job.Spec = "0 0 1 1 *"
	rec := registerJob(t, job)
	require.NoError(t, rec.Activate())
	assertNoRun(t, runs, 1200*time.Millisecond)

	require.NoError(t, rec.UpdateSpec(everySecond))
	assert.Equal(t, everySecond, Records.FindByHandler("respec").Spec)
	receiveRun(t, runs, 2*time.Second)
}

func TestUpdateSpecInactive(t *testing.T) {
	clearTables(t)
	job, runs := signalJob("respec-inactive", nil)
	rec := registerJob(t, job)
	require.NoError(t, rec.UpdateSpec("0 0 * * *"))
	stored := Records.FindByHandler("respec-inactive")
	assert.Equal(t, "0 0 * * *", stored.Spec)
	assert.False(t, stored.Active)
	assertNoRun(t, runs, 1200*time.Millisecond)
}
