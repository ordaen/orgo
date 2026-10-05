package cron

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ordaen/orgo/logs"
)

// Jobs type
type Jobs struct {
	sync.RWMutex
	jobs map[string]*Job
	// groupMu serializes the group changes
	groupMu sync.Mutex
}

// Register adds the job and creates its record, or activates it when its record is active. The Type of the job
// is its group, see RegisterGroup. The database must be connected. The stored name and type of the record are
// updated, its spec and active state are kept. A record left running by a previous process is marked not running,
// its run was interrupted, which assumes a single application instance, see the package documentation.
func (c *Jobs) Register(job Job) error {
	return c.register(job, false)
}

// register registers the job, replacing a registered job with the same ID and Type when replace is set.
func (c *Jobs) register(job Job, replace bool) error {
	if job.ID == "" {
		return errors.New("cron job ID can't be blank")
	}
	c.Lock()
	prev, ok := c.jobs[job.ID]
	if ok && (!replace || prev.Type != job.Type) {
		c.Unlock()
		if prev.Type != job.Type {
			return fmt.Errorf("cron entry with ID '%s' already added to '%s'", job.ID, prev.Type)
		}
		return fmt.Errorf("cron entry with ID '%s' already added", job.ID)
	}
	job.sched = &schedule{}
	c.jobs[job.ID] = &job
	c.Unlock()
	if ok {
		// the replaced job finishes its running run
		prev.Stop()
	} else {
		logs.Info(fmt.Sprintf("Registering Cron job: %s - %s", job.ID, job.Name))
	}

	rec := Records.FindByHandler(job.ID)
	if !rec.ID.Valid() {
		rec.Handler = job.ID
		rec.Spec = job.Spec
		rec.Name = job.Name
		rec.Type = job.Type
		created, err := Records.Create(rec)
		if err != nil {
			return fmt.Errorf("create cron record %s: %w", job.ID, err)
		}
		if job.Active {
			return created.Activate()
		}
		return nil
	}
	fields := []string{}
	if rec.Running {
		rec.Running, rec.LogID = false, 0
		fields = append(fields, "running", "log_id")
	}
	if rec.Name != job.Name || rec.Type != job.Type {
		rec.Name, rec.Type = job.Name, job.Type
		fields = append(fields, "name", "type")
	}
	if len(fields) > 0 {
		updated, err := Records.Update(rec, fields...)
		if err != nil {
			return fmt.Errorf("update cron record %s: %w", job.ID, err)
		}
		rec = updated
	}
	if rec.Active {
		return rec.Activate()
	}
	return nil
}

// Unregister stops the job and deletes its record with its logs
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
	return deleteJobRecords("handler = $1", id)
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
