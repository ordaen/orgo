// Package cron runs registered jobs by their cron specs, stores their state in cron_records and their runs
// with their messages in cron_logs and cron_log_messages. A job runs only while its record is active.
//
// The jobs are meant to be scheduled by a single application instance. A run marks its record running with
// a conditional update, so a job does not run twice at the same time, ErrRunning is returned instead. But
// Register marks a record left running as not running, assuming its process stopped during the run: when
// several instances register the same jobs, a starting instance can release the run of another one.
package cron

// Global is the jobs of the application.
var Global = New()

// Func is the function of a job, called with the log of its run.
type Func func(*CronLog)

// Logger logs the messages of a job run, it is implemented by CronLog.
type Logger interface {
	// UpdateMessage sets the message of the run, formatted with the args when they are given.
	UpdateMessage(string, ...any)
	// AddMessage adds a message to the run, formatted with the args when they are given.
	AddMessage(string, ...any)
	// AddErrorString adds an error message to the run, formatted with the args when they are given.
	AddErrorString(string, ...any)
	// AddError adds the error to the run.
	AddError(error)
}

// New returns new Jobs without jobs.
func New() *Jobs {
	return &Jobs{jobs: make(map[string]*Job)}
}
