// Package actions records the actions users run: an ActionLog with the user (the issuer), the request, the tasks
// of the action with their status, the progress and the messages. Action runs the tasks in order and saves the log
// after each, in the caller's goroutine (Run) or in its own (Start); the log publishes its changes, so a page
// following it shows the tasks as they run.
//
// The action_logs table is registered by RegisterSchema, called before pg.Connect.
package actions

import (
	"github.com/ordaen/orgo/logger"
)

// Log is the record of an action.
type Log = ActionLog

// The levels of the messages of a log.
const (
	INFO  = logger.INFO
	WARN  = logger.WARN
	DEBUG = logger.DEBUG
	ERROR = logger.ERROR
)

// Status is the status of a log or of one of its tasks.
type Status string

// The statuses: a log is pending until its first task, running, then complete or failed; a task is also cancelled
// (not run after a failure) or skipped (its function returned ErrSkip).
const (
	Pending   Status = "pending"
	Running   Status = "running"
	Complete  Status = "complete"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
	Skipped   Status = "skipped"
)

// Finished reports whether the status is a final one of a log: complete or failed.
func (s Status) Finished() bool {
	return s == Complete || s == Failed
}

// ModelAction is what an action did to a record of its log's Models.
type ModelAction string

// The model actions.
const (
	Created ModelAction = "created"
	Updated ModelAction = "updated"
	Deleted ModelAction = "deleted"
)

// Task is a step of an action.
type Task struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   Status `json:"status,omitempty"`
	Progress int    `json:"progress,omitempty"`
}

// Model is a record an action created, updated or deleted.
type Model struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Action ModelAction `json:"action"`
}
