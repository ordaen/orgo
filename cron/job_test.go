package cron

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobRunSuccess(t *testing.T) {
	clearTables(t)
	job, runs := signalJob("success", nil)
	rec := registerJob(t, job)

	require.NoError(t, rec.Run())
	log := receiveRun(t, runs, time.Second)
	assert.True(t, log.ID.Valid(), "the job gets its stored log")

	rec = Records.FindByHandler("success")
	assert.False(t, rec.Running)
	assert.False(t, rec.LogID.Valid())
	assert.Empty(t, rec.LastError)
	assert.WithinDuration(t, time.Now(), rec.LastRun, 5*time.Second)
	assert.True(t, rec.NextRun.IsZero(), "a manual run does not set the next run")

	stored := Logs.FindByID(log.ID)
	assert.Equal(t, "complete", stored.Status)
	msgs := LogMessages.FindMany("log_id = ?", log.ID)
	require.Len(t, msgs, 1)
	assert.Equal(t, "done success", msgs[0].Message)
}

func TestJobRunPanic(t *testing.T) {
	clearTables(t)
	rec := registerJob(t, Job{ID: "panic", Name: "panic", Spec: everySecond, Func: func(*CronLog) { panic("test panic") }})

	assert.EqualError(t, rec.Run(), "test panic")
	rec = Records.FindByHandler("panic")
	assert.Equal(t, "test panic", rec.LastError)
	assert.False(t, rec.Running)
	log := Logs.FindWhere("handler = ?", "panic")
	assert.Equal(t, "complete", log.Status)
	assert.Positive(t, LogMessages.CountWhere("log_id = ? AND error", log.ID))
}

func TestJobExecute(t *testing.T) {
	clearTables(t)
	release := make(chan struct{})
	job, runs := signalJob("execute", release)
	rec := registerJob(t, job)

	log, err := rec.Execute()
	require.NoError(t, err)
	assert.Equal(t, log.ID, receiveRun(t, runs, time.Second).ID)
	running := Records.FindByHandler("execute")
	assert.True(t, running.Running)
	assert.Equal(t, log.ID, running.LogID)

	// one run at a time
	_, err = rec.Execute()
	assert.ErrorIs(t, err, ErrRunning)
	assert.ErrorIs(t, rec.Run(), ErrRunning)

	close(release)
	rec = waitRecord(t, "execute", func(r *CronRecord) bool { return !r.Running })
	assert.False(t, rec.LogID.Valid())
	assert.Equal(t, 1, Logs.CountWhere("handler = ?", "execute"), "the refused runs have no log")
}

func TestJobRunConcurrent(t *testing.T) {
	clearTables(t)
	release := make(chan struct{})
	job, runs := signalJob("concurrent", release)
	rec := registerJob(t, job)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for range 5 {
		wg.Go(func() {
			err := rec.Run()
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		})
	}
	receiveRun(t, runs, time.Second)
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	refused := 0
	for _, err := range errs {
		if errors.Is(err, ErrRunning) {
			refused++
		} else {
			assert.NoError(t, err)
		}
	}
	assert.Equal(t, 4, refused, "only one of the concurrent runs runs")
	assert.Len(t, runs, 0)
}

func TestNoManualExecution(t *testing.T) {
	clearTables(t)
	job, _ := signalJob("auto", nil)
	job.Options.NoManualExecution = true
	rec := registerJob(t, job)
	assert.EqualError(t, rec.Run(), "no manual execution allowed for this cron job")
	_, err := rec.Execute()
	assert.EqualError(t, err, "no manual execution allowed for this cron job")
}

func TestJobNotRegistered(t *testing.T) {
	rec := &CronRecord{Handler: "missing"}
	assert.ErrorIs(t, rec.Run(), errCronJobNotFound)
	_, err := rec.Execute()
	assert.ErrorIs(t, err, errCronJobNotFound)
	assert.ErrorIs(t, rec.Activate(), errCronJobNotFound)
	assert.ErrorIs(t, rec.Deactivate(), errCronJobNotFound)
	assert.Error(t, (&Job{ID: "x"}).ResetSpec(everySecond))
}

func TestJobNext(t *testing.T) {
	j := &Job{Spec: "0 0 * * *"}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), j.Next(now))
	assert.Equal(t, 12*time.Hour, j.NextDuration(now))
	assert.Len(t, j.NextN(now, 3), 3)
	bad := &Job{Spec: "bad"}
	assert.True(t, bad.Next(now).IsZero())
	assert.Nil(t, bad.NextN(now, 2))
}
