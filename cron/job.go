package cron

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gorhill/cronexpr"
	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/pg"
)

// ErrRunning is returned when a job is run while it is running, by this or another process.
var ErrRunning = errors.New("cron job is already running")

type JobOptions struct {
	NoSpecChange      bool
	NoManualExecution bool
}

// Job type
type Job struct {
	ID   string
	Name string
	Spec string
	// Type is the group of the job, like "system" or the name of a plugin, see RegisterGroup
	Type      string
	Active    bool
	CanEnable func() error
	Func      Func
	Options   JobOptions

	// sched is the schedule of a registered job, it is shared by the copies of the job
	sched *schedule
}

// schedule runs a job at the times of its spec until it is stopped. Its loop owns its expression,
// an expression is not safe for concurrent use: its Next updates it.
type schedule struct {
	mu     sync.Mutex
	spec   string
	cancel context.CancelFunc
	// loops are the running loops, a stopped loop returns after its running run
	loops sync.WaitGroup
}

// parseSpec parses the cron spec.
func parseSpec(spec string) (*cronexpr.Expression, error) {
	return cronexpr.Parse(spec)
}

// expression returns a new expression of the spec of the running schedule, or of the job when it is not scheduled.
func (j *Job) expression() *cronexpr.Expression {
	spec := j.Spec
	if j.sched != nil {
		j.sched.mu.Lock()
		if j.sched.spec != "" {
			spec = j.sched.spec
		}
		j.sched.mu.Unlock()
	}
	exp, _ := parseSpec(spec)
	return exp
}

// NextDuration returns duration to the closes next point starting from 't'
func (j *Job) NextDuration(t time.Time) time.Duration {
	return j.Next(t).Sub(t)
}

// Next returns the closest time instant immediately following `t`, the zero time when there is none
// or the spec is invalid
func (j *Job) Next(t time.Time) time.Time {
	if exp := j.expression(); exp != nil {
		return exp.Next(t)
	}
	return time.Time{}
}

// NextN returns a slice of `n` closest time instants immediately following `t`
func (j *Job) NextN(t time.Time, n uint) []time.Time {
	if exp := j.expression(); exp != nil {
		return exp.NextN(t, n)
	}
	return nil
}

// ResetSpec schedules the registered job by the spec, replacing its schedule
func (j *Job) ResetSpec(spec string) error {
	exp, err := parseSpec(spec)
	if err != nil {
		return err
	}
	if j.sched == nil {
		return fmt.Errorf("cron job '%s' is not registered", j.ID)
	}
	j.sched.mu.Lock()
	defer j.sched.mu.Unlock()
	if j.sched.cancel != nil {
		j.sched.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.sched.spec, j.sched.cancel = spec, cancel
	j.sched.loops.Go(func() { j.loop(ctx, exp) })
	return nil
}

// Stop stops the schedule of the job. It does not wait for a running job, it finishes its run.
func (j *Job) Stop() {
	if j.sched == nil {
		return
	}
	j.sched.mu.Lock()
	defer j.sched.mu.Unlock()
	if j.sched.cancel != nil {
		j.sched.cancel()
	}
	j.sched.spec, j.sched.cancel = "", nil
}

// loop runs the job at the times of exp until ctx is canceled. A run that is still running at the next time
// delays the next run, the times passed meanwhile are skipped.
func (j *Job) loop(ctx context.Context, exp *cronexpr.Expression) {
	for {
		next := exp.Next(time.Now())
		if next.IsZero() {
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := j.run(ctx, exp); err != nil && !errors.Is(err, ErrRunning) {
			logs.Error(err, fmt.Sprintf("cron job %s:", j.ID))
		}
	}
}

// run runs the job and records the run. exp is the expression of the schedule running it, nil for a manual run.
func (j *Job) run(ctx context.Context, exp *cronexpr.Expression) error {
	rec, log, err := j.begin()
	if err != nil {
		return err
	}
	return j.finish(ctx, rec, log, exp)
}

// runInBackground begins the run and runs the job in the background.
func (j *Job) runInBackground() (*CronLog, error) {
	rec, log, err := j.begin()
	if err != nil {
		return nil, err
	}
	go j.finish(context.Background(), rec, log, nil)
	return log, nil
}

// begin marks the record of the job running and creates the log of the run. The record is marked with a
// conditional update, so a job runs once at a time, also when it is run by several processes.
func (j *Job) begin() (*CronRecord, *CronLog, error) {
	rec := Records.FindByHandler(j.ID)
	if !rec.ID.Valid() {
		return nil, nil, fmt.Errorf("cron job '%s' not found", j.ID)
	}
	n, err := pg.Exec(`UPDATE cron_records SET running = TRUE, updated = now() WHERE id = $1 AND running IS NOT TRUE`, rec.ID)
	if err != nil {
		return nil, nil, err
	}
	if n == 0 {
		return nil, nil, ErrRunning
	}
	rec.Running = true

	log := NewLog(j.Name, j.ID)
	if err := log.Create(); err != nil {
		j.release(rec)
		return nil, nil, err
	}
	rec.LogID = log.ID
	if _, err := Records.Update(rec, "log_id"); err != nil {
		j.release(rec)
		return nil, nil, err
	}
	return rec, log, nil
}

// release marks the record not running after a run could not begin.
func (j *Job) release(rec *CronRecord) {
	rec.Running, rec.LogID = false, 0
	if _, err := Records.Update(rec, "running", "log_id"); err != nil {
		logs.Error(err, fmt.Sprintf("cron job %s release:", j.ID))
	}
}

// finish runs the job function, completes its log and updates its record. The next run is set when the schedule
// exp running it is not stopped.
func (j *Job) finish(ctx context.Context, rec *CronRecord, log *CronLog, exp *cronexpr.Expression) error {
	err := j.runWithRecovery(log)
	log.SetComplete(err)

	rec.Running = false
	rec.LastRun = time.Now()
	rec.LastError = ""
	rec.LogID = 0
	if err != nil {
		rec.LastError = err.Error()
	}
	fields := []string{"running", "last_run", "last_error", "log_id"}
	if exp != nil && ctx.Err() == nil {
		rec.NextRun = exp.Next(rec.LastRun)
		fields = append(fields, "next_run")
	}
	if _, uerr := Records.Update(rec, fields...); uerr != nil {
		logs.Error(uerr, fmt.Sprintf("cron job %s update record:", j.ID))
	}
	return err
}

func (j *Job) runWithRecovery(log *CronLog) (err error) {
	defer logs.Recover(func(r any) {
		err = fmt.Errorf("%v", r)
		if log != nil {
			if logs.SentryOn() {
				log.AddErrorString("cron: panic running job: %v", r)
			} else {
				log.AddErrorString("cron: panic running job: %v\n%s", r, debug.Stack())
			}
		}
	})

	j.Func(log)
	return
}
