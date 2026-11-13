package cron

import (
	"sync"
	"testing"
	"time"

	"github.com/ordaen/orgo/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJobRunEvents checks a run publishes its record, log and messages, for the live job pages.
func TestJobRunEvents(t *testing.T) {
	clearTables(t)
	var mu sync.Mutex
	got := map[string]int{}
	record := func(hub string) events.EventFunc {
		return func(e events.Event) {
			mu.Lock()
			defer mu.Unlock()
			got[hub+" "+e.Name]++
		}
	}
	for _, name := range []string{"CronRecord", "CronLog", "CronLogMessage"} {
		t.Cleanup(events.Creates.Sub(name, record("create")))
		t.Cleanup(events.Updates.Sub(name, record("update")))
	}
	job, runs := signalJob("events", nil)
	rec := registerJob(t, job)
	require.NoError(t, rec.Run())
	receiveRun(t, runs, time.Second)

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		// the record: running with its log, then done
		return got["update CronRecord"] >= 2 && got["create CronLog"] == 1 && got["update CronLog"] >= 1 &&
			got["create CronLogMessage"] == 1
	}, 2*time.Second, 10*time.Millisecond, "%v", got)
}
