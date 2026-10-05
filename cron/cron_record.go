package cron

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/repo"
)

var Records = repo.Register(&cronRecords{})

type cronRecords struct {
	repo.Base[*CronRecord]
}

// FindByHandler returns *CronRecord by handler
func (r *cronRecords) FindByHandler(h string) *CronRecord {
	return r.FindWhere("handler = ?", h)
}

// CronRecord model
type CronRecord struct {
	model.Base[model.ID]

	LogID     model.ID  `json:"log_id,omitempty"`
	Type      string    `json:"type,readonly"`
	Handler   string    `json:"handler,readonly"`
	Name      string    `json:"name,readonly"`
	Spec      string    `json:"spec"`
	LastError string    `json:"last_error,readonly"`
	NextRun   time.Time `json:"next_run,readonly"`
	LastRun   time.Time `json:"last_run,readonly"`
	Active    bool      `json:"active,readonly"`
	Running   bool      `json:"running,readonly"`
}

func (m *CronRecord) TableName() string { return "cron_records" }

func (m *CronRecord) Options() *JobOptions {
	job := m.job()
	if job != nil {
		return &job.Options
	}
	return &JobOptions{}
}

func (m *CronRecord) canUpdateSpec(spec string) error {
	if m.Options().NoSpecChange {
		return errors.New("no spec change allowed for this cron job")
	}
	_, err := parseSpec(spec)
	return err
}

// UpdateSpec stores the spec and reschedules the job when it is active
func (m *CronRecord) UpdateSpec(spec string) error {
	if err := m.canUpdateSpec(spec); err != nil {
		return err
	}

	m.Spec = spec
	updated, err := Records.Update(m, "spec")
	if err != nil {
		return err
	}
	*m = *updated
	if m.Active {
		return m.Activate()
	}
	return nil
}

// Registered reports whether the job of the record is registered, the job of a disabled plugin is not
func (m *CronRecord) Registered() bool {
	return m.job() != nil
}

func (m *CronRecord) job() *Job {
	return Global.Job(m.Handler)
}

var errCronJobNotFound = errors.New("cron job not found")

// Activate schedules the job by the spec of the record and stores it active with its next run
func (m *CronRecord) Activate() error {
	job := m.job()
	if job == nil {
		return errCronJobNotFound
	}

	if job.CanEnable != nil {
		if err := job.CanEnable(); err != nil {
			return err
		}
	}

	next, err := Global.Next(m.Spec)
	if err != nil {
		if err.Error() == "missing field(s)" {
			return errors.New("invalid spec format")
		}
		return err
	}

	logs.Info(fmt.Sprintf("Activating Cron job: %s - %s", job.ID, job.Name))

	m.Active = true
	m.NextRun = next
	m.LastError = ""
	updated, err := Records.Update(m, "active", "next_run", "last_error")
	if err != nil {
		return err
	}
	*m = *updated
	return job.ResetSpec(m.Spec)
}

// Deactivate stops the schedule of the job and stores it not active. A running job finishes its run.
func (m *CronRecord) Deactivate() error {
	job := m.job()
	if job == nil {
		return errCronJobNotFound
	}

	logs.Info(fmt.Sprintf("Deactivating Cron job: %s - %s", job.ID, job.Name))
	job.Stop()

	m.Active = false
	m.NextRun = time.Time{}
	updated, err := Records.Update(m, "active", "next_run")
	if err != nil {
		return err
	}
	*m = *updated
	return nil
}

// Run runs the job and waits for it, it returns ErrRunning when the job is running
func (m *CronRecord) Run() error {
	if m.Options().NoManualExecution {
		return errors.New("no manual execution allowed for this cron job")
	}
	job := m.job()
	if job == nil {
		return errCronJobNotFound
	}
	return job.run(context.Background(), nil)
}

// Execute runs the job in the background and returns the log of its run,
// it returns ErrRunning when the job is running
func (m *CronRecord) Execute() (*CronLog, error) {
	if m.Options().NoManualExecution {
		return nil, errors.New("no manual execution allowed for this cron job")
	}
	job := m.job()
	if job == nil {
		return nil, errCronJobNotFound
	}
	return job.runInBackground()
}
