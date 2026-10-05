// Package cron runs registered jobs by their cron specs, stores their state in cron_records and their runs
// with their messages in cron_logs and cron_log_messages. A job runs only while its record is active.
//
// The jobs are meant to be scheduled by a single application instance. A run marks its record running with
// a conditional update, so a job does not run twice at the same time, ErrRunning is returned instead. But
// Register marks a record left running as not running, assuming its process stopped during the run: when
// several instances register the same jobs, a starting instance can release the run of another one.
package cron

// Global global application cron manager
var Global = New()

// Func type for cron function
type Func func(*CronLog)

type Logger interface {
	UpdateMessage(string, ...any)
	AddMessage(string, ...any)
	AddErrorString(string, ...any)
	AddError(error)
}

// New returns new Jobs
func New() *Jobs {
	return &Jobs{jobs: make(map[string]*Job)}
}
