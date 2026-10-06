package cron

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/pg"
)

// RegisterGroup syncs the jobs of the group with Global, see Jobs.RegisterGroup.
func RegisterGroup(group string, jobs ...Job) error {
	return Global.RegisterGroup(group, jobs...)
}

// DisableGroup stops the jobs of the group in Global and keeps their records, see Jobs.DisableGroup.
func DisableGroup(group string) {
	Global.DisableGroup(group)
}

// RemoveGroup stops the jobs of the group in Global and deletes their records, see Jobs.RemoveGroup.
func RemoveGroup(group string) error {
	return Global.RemoveGroup(group)
}

// RegisterGroup syncs the jobs of the group, like the system jobs of an application or the jobs of a plugin.
// The group is the Type of the jobs and their records. It registers the jobs, replacing the definitions
// of the registered ones, which are rescheduled when they are active, and it stops the registered jobs of
// the group which are not in jobs and deletes their records with their logs. The stored specs and active
// states of the jobs are kept. It can be called again, like when a plugin is enabled or reloaded.
// The database must be connected.
//
//	cron.RegisterGroup("system", systemJobs...)
func (c *Jobs) RegisterGroup(group string, jobs ...Job) error {
	if group == "" {
		return errors.New("cron group can't be blank")
	}
	c.groupMu.Lock()
	defer c.groupMu.Unlock()

	var errs []error
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		job.Type = group
		ids = append(ids, job.ID)
		if err := c.register(job, true); err != nil {
			errs = append(errs, err)
		}
	}
	for _, j := range c.groupJobs(group) {
		if !slices.Contains(ids, j.ID) {
			c.detach(j)
		}
	}
	if err := deleteJobRecords("type = $1 AND handler <> ALL($2)", group, ids); err != nil {
		errs = append(errs, fmt.Errorf("delete removed jobs of %s: %w", group, err))
	}
	return errors.Join(errs...)
}

// DisableGroup stops and unregisters the jobs of the group, like when a plugin is disabled. Their records
// are kept with their specs and active states, RegisterGroup resumes them. The running jobs finish their runs.
func (c *Jobs) DisableGroup(group string) {
	c.groupMu.Lock()
	defer c.groupMu.Unlock()
	for _, j := range c.groupJobs(group) {
		c.detach(j)
	}
}

// RemoveGroup stops and unregisters the jobs of the group and deletes the records of the group with their logs,
// like when a plugin is removed.
func (c *Jobs) RemoveGroup(group string) error {
	if group == "" {
		return errors.New("cron group can't be blank")
	}
	c.groupMu.Lock()
	defer c.groupMu.Unlock()
	for _, j := range c.groupJobs(group) {
		c.detach(j)
	}
	return deleteJobRecords("type = $1", group)
}

// groupJobs returns the registered jobs of the group.
func (c *Jobs) groupJobs(group string) []*Job {
	c.RLock()
	defer c.RUnlock()
	var res []*Job
	for _, j := range c.jobs {
		if j.Type == group {
			res = append(res, j)
		}
	}
	return res
}

// detach stops the job and removes it from the registered jobs, keeping its record.
func (c *Jobs) detach(j *Job) {
	logs.Info(fmt.Sprintf("Removing Cron job: %s", j.ID))
	j.Stop()
	c.Lock()
	if c.jobs[j.ID] == j {
		delete(c.jobs, j.ID)
	}
	c.Unlock()
}

// deleteJobRecords deletes the cron records matching the where condition, with $ placeholders,
// and the logs and the log messages of their handlers in one statement.
func deleteJobRecords(where string, args ...any) error {
	_, err := pg.Exec(`WITH removed AS (
			DELETE FROM cron_records WHERE `+where+` RETURNING handler
		), removed_logs AS (
			DELETE FROM cron_logs WHERE handler IN (SELECT handler FROM removed) RETURNING id
		)
		DELETE FROM cron_log_messages WHERE log_id IN (SELECT id FROM removed_logs)`, args...)
	return err
}
