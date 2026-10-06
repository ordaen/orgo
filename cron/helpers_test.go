package cron

import (
	"testing"
	"time"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
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
	t.Cleanup(func() {
		stopJobs(t, func(j *Job) bool { return j.ID == job.ID })
		Global.Unregister(job.ID)
	})
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

// waitStopped waits until the schedule of the job is stopped and its last run finished,
// and drains the runs, the job does not run anymore.
func waitStopped(t *testing.T, job *Job, runs <-chan *CronLog) {
	t.Helper()
	assertNotScheduled(t, job)
	waitLoops(t, job)
	for len(runs) > 0 {
		<-runs
	}
}

// waitLoops waits until the schedule loops of the job returned, after their running runs.
func waitLoops(t *testing.T, job *Job) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		job.sched.loops.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("schedule not stopped")
	}
}

// stopJobs stops the registered jobs matching match and waits for their running runs,
// so they do not write to the tables of the next test.
func stopJobs(t *testing.T, match func(*Job) bool) {
	t.Helper()
	Global.RLock()
	var jobs []*Job
	for _, j := range Global.jobs {
		if match(j) {
			jobs = append(jobs, j)
		}
	}
	Global.RUnlock()
	for _, j := range jobs {
		j.Stop()
		waitLoops(t, j)
	}
}

// assertNotScheduled checks the job has no schedule, it does not run.
func assertNotScheduled(t *testing.T, job *Job) {
	t.Helper()
	job.sched.mu.Lock()
	defer job.sched.mu.Unlock()
	assert.Nil(t, job.sched.cancel, "the job is not scheduled")
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
