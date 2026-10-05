package cron

import (
	"fmt"
	"sync"
	"time"

	"github.com/ordaen/orgo/logs"
)

// Jobs type
type Jobs struct {
	sync.RWMutex
	jobs map[string]*Job
}

// Register adds the job and creates its record, or activates it when its record is active. The database must be
// connected. A record left running by a previous process is marked not running, its run was interrupted,
// which assumes a single application instance, see the package documentation.
func (c *Jobs) Register(job Job) error {
	c.Lock()
	if _, ok := c.jobs[job.ID]; ok {
		c.Unlock()
		return fmt.Errorf("cron entry with ID '%s' already added", job.ID)
	}

	logs.Info(fmt.Sprintf("Registering Cron job: %s - %s", job.ID, job.Name))

	job.sched = &schedule{}
	c.jobs[job.ID] = &job
	c.Unlock()

	rec := Records.FindByHandler(job.ID)
	if !rec.ID.Valid() {
		rec.Handler = job.ID
		rec.Spec = job.Spec
		rec.Name = job.Name
		rec.Type = job.Type
		rec.Plugin = job.Plugin
		created, err := Records.Create(rec)
		if err != nil {
			return fmt.Errorf("create cron record %s: %w", job.ID, err)
		}
		if job.Active {
			return created.Activate()
		}
		return nil
	}
	if rec.Running {
		rec.Running, rec.LogID = false, 0
		if _, err := Records.Update(rec, "running", "log_id"); err != nil {
			return fmt.Errorf("update cron record %s: %w", job.ID, err)
		}
	}
	if rec.Active {
		return rec.Activate()
	}
	return nil
}

// Unregister stops the job and deletes its record
func (c *Jobs) Unregister(id string) error {
	c.Lock()
	j, ok := c.jobs[id]
	delete(c.jobs, id)
	c.Unlock()
	if !ok {
		return nil
	}
	logs.Info(fmt.Sprintf("Removing Cron job: %s", id))
	j.Stop()

	rec := Records.FindByHandler(id)
	if rec.ID.Valid() {
		return Records.Delete(rec)
	}
	return nil
}

// Stop stops the schedules of all jobs without changing their records, like on shutdown.
// The running jobs finish their runs.
func (c *Jobs) Stop() {
	c.RLock()
	defer c.RUnlock()
	for _, j := range c.jobs {
		j.Stop()
	}
}

func (c *Jobs) Job(id string) *Job {
	c.RLock()
	defer c.RUnlock()
	return c.jobs[id]
}

// Next returning next time for specified spec
func (c *Jobs) Next(spec string) (time.Time, error) {
	exp, err := parseSpec(spec)
	if err != nil {
		return time.Time{}, err
	}
	return exp.Next(time.Now()), nil
}
