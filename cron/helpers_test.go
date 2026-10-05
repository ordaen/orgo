package cron

import (
	"testing"
	"time"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/require"
)

// everySecond is a spec with the seconds field running every second.
const everySecond = "* * * * * * *"

func clearTables(t *testing.T) {
	t.Helper()
	require.NoError(t, pg.ClearTables("cron_records", "cron_logs", "cron_log_messages"))
}

// registerJob registers the job with Global until the test ends and returns its record.
func registerJob(t *testing.T, job Job) *CronRecord {
	t.Helper()
	require.NoError(t, Global.Register(job))
	t.Cleanup(func() { Global.Unregister(job.ID) })
	rec := Records.FindByHandler(job.ID)
	require.True(t, rec.ID.Valid())
	return rec
}

// signalJob returns a job sending its runs to the returned channel, it waits for release when it is not nil.
func signalJob(id string, release <-chan struct{}) (Job, <-chan *CronLog) {
	runs := make(chan *CronLog, 100)
	return Job{ID: id, Name: id, Spec: everySecond, Type: "test", Func: func(log *CronLog) {
		runs <- log
		if release != nil {
			<-release
		}
		log.AddMessage("done %s", id)
	}}, runs
}

func receiveRun(t *testing.T, runs <-chan *CronLog, wait time.Duration) *CronLog {
	t.Helper()
	select {
	case log := <-runs:
		return log
	case <-time.After(wait):
		t.Fatal("job did not run")
		return nil
	}
}

func assertNoRun(t *testing.T, runs <-chan *CronLog, wait time.Duration) {
	t.Helper()
	select {
	case <-runs:
		t.Fatal("unexpected run")
	case <-time.After(wait):
	}
}

// waitRecord waits until the record of the handler matches cond.
func waitRecord(t *testing.T, handler string, cond func(*CronRecord) bool) *CronRecord {
	t.Helper()
	var rec *CronRecord
	require.Eventually(t, func() bool {
		rec = Records.FindByHandler(handler)
		return cond(rec)
	}, 3*time.Second, 10*time.Millisecond)
	return rec
}
