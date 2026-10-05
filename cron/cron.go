// Package cron runs registered jobs by their cron specs, stores their state in cron_records and their runs
// with their messages in cron_logs and cron_log_messages. A job runs only while its record is active.
//
// The jobs are meant to be scheduled by a single application instance. A run marks its record running with
// a conditional update, so a job does not run twice at the same time, ErrRunning is returned instead. But
// Register marks a record left running as not running, assuming its process stopped during the run: when
// several instances register the same jobs, a starting instance can release the run of another one.
package cron

import (
	"errors"
	"fmt"
	"log"
)

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

// RegisterJobs registers the jobs of the plugin with Global and deletes the records of the plugin jobs
// that are not registered anymore. The database must be connected.
func RegisterJobs(plugin string, jobs []Job) error {
	var errs []error
	keys := make([]string, 0, len(jobs))
	for _, job := range jobs {
		keys = append(keys, job.ID)
		job.Plugin = plugin
		if err := Global.Register(job); err != nil {
			errs = append(errs, err)
		}
	}

	recs, err := Records.Query().Where("plugin = ? AND handler <> ALL(?)", plugin, keys).Select()
	if err != nil {
		errs = append(errs, fmt.Errorf("find removed cron records: %w", err))
	}
	for _, v := range recs {
		if err := Records.Delete(v); err != nil {
			errs = append(errs, fmt.Errorf("delete cron record %s: %w", v.Handler, err))
		}
	}
	err = errors.Join(errs...)
	if err != nil {
		log.Println("Error registering cron jobs", err)
	}
	return err
}
