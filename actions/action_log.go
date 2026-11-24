package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

// Logs is the repository of the action logs, in the action_logs table; it publishes their changes.
var Logs = repo.Register(&logsStore{}, repo.WithEvents())

type logsStore struct {
	repo.Base[*ActionLog]
}

var _ pg.BeforeCreateHook = (*ActionLog)(nil)

// ActionLogData is the data of a log: the request of the action.
type ActionLogData struct {
	Request json.RawMessage `json:"request,omitempty"`
}

// ActionLog is the record of an action run by a user (the issuer) on a record (the owner): its tasks with their
// status, the progress in percent, the messages. Its methods change it and return it; Save stores it.
type ActionLog struct {
	model.Base[model.ID]

	Name      string              `json:"name"`
	OwnerID   string              `json:"owner_id"`
	OwnerType string              `json:"owner_type"`
	OwnerName string              `json:"owner_name,omitempty"`
	Status    Status              `json:"status"`
	Progress  int                 `json:"progress"`
	Data      ActionLogData       `json:"data"`
	Tasks     []Task              `json:"tasks,omitempty"`
	Messages  []logger.LogMessage `json:"messages,omitempty"`
	Models    []Model             `json:"models,omitempty"`
	Issuer    *types.User         `json:"issuer,omitempty"`
}

// TableName returns "action_logs".
func (*ActionLog) TableName() string { return "action_logs" }

// SortBy orders the lists by the last update, then by id: logs updated at the same time stay in the same page.
func (*ActionLog) SortBy() string { return "updated DESC, id DESC" }

// BeforeCreate makes a log without a status pending.
func (m *ActionLog) BeforeCreate(context.Context, pg.Tx) error {
	if m.Status == "" {
		m.Status = Pending
	}
	return nil
}

// AddRequest stores the request of the action, in JSON. Mask its secrets first.
func (m *ActionLog) AddRequest(req any) *ActionLog {
	m.Data.Request, _ = json.Marshal(req)
	return m
}

// AddMessage adds a message of the level, msg a fmt format of args; an empty one is ignored.
func (m *ActionLog) AddMessage(level logger.LogLevel, msg string, args ...any) *ActionLog {
	if msg == "" {
		return m
	}
	m.Messages = append(m.Messages, logger.LogMessage{Message: fmt.Sprintf(msg, args...), Time: time.Now(),
		Level: level})
	return m
}

// Info adds an info message.
func (m *ActionLog) Info(msg string, args ...any) *ActionLog {
	return m.AddMessage(INFO, msg, args...)
}

// Warn adds a warning message.
func (m *ActionLog) Warn(msg string, args ...any) *ActionLog {
	return m.AddMessage(WARN, msg, args...)
}

// Debug adds a debug message.
func (m *ActionLog) Debug(msg string, args ...any) *ActionLog {
	return m.AddMessage(DEBUG, msg, args...)
}

// Error adds an error message.
func (m *ActionLog) Error(msg string, args ...any) *ActionLog {
	return m.AddMessage(ERROR, msg, args...)
}

// Err adds the error as an error message; nil adds nothing.
func (m *ActionLog) Err(err error) *ActionLog {
	if err != nil {
		return m.AddMessage(ERROR, "%s", err.Error())
	}
	return m
}

// WithOwner sets the record the action is about.
func (m *ActionLog) WithOwner(id, ownerType, name string) *ActionLog {
	m.OwnerID, m.OwnerType, m.OwnerName = id, ownerType, name
	return m
}

// WithIssuer sets the user running the action.
func (m *ActionLog) WithIssuer(user *types.User) *ActionLog {
	m.Issuer = user
	return m
}

// SetTasks replaces the tasks with copies of these, pending.
func (m *ActionLog) SetTasks(tasks ...Task) *ActionLog {
	m.Tasks = make([]Task, 0, len(tasks))
	for _, t := range tasks {
		m.Tasks = append(m.Tasks, Task{ID: t.ID, Name: t.Name, Status: Pending})
	}
	return m
}

// AddTask adds a pending task, unless one has the ID.
func (m *ActionLog) AddTask(id, name string) *ActionLog {
	if m.taskIdx(id) == -1 {
		m.Tasks = append(m.Tasks, Task{ID: id, Name: name, Status: Pending})
	}
	return m
}

func (m *ActionLog) taskIdx(id string) int {
	for i, t := range m.Tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// StartTask marks the task running.
func (m *ActionLog) StartTask(id string) *ActionLog {
	if i := m.taskIdx(id); i != -1 {
		m.Tasks[i].Status = Running
	}
	return m
}

// EndTask sets the status of the task and the progress of the log to the part of the tasks up to it.
func (m *ActionLog) EndTask(id string, status Status) *ActionLog {
	return m.endTask(id, status, true)
}

// EndTaskNoProgress sets the status of the task, the progress unchanged (a failed task).
func (m *ActionLog) EndTaskNoProgress(id string, status Status) *ActionLog {
	return m.endTask(id, status, false)
}

func (m *ActionLog) endTask(id string, status Status, progress bool) *ActionLog {
	i := m.taskIdx(id)
	if i == -1 {
		return m
	}
	m.Tasks[i].Status = status
	if progress {
		m.Progress = 100 * (i + 1) / len(m.Tasks)
	}
	return m
}

// TaskProgress sets the progress of the task, in percent.
func (m *ActionLog) TaskProgress(id string, progress int) *ActionLog {
	if i := m.taskIdx(id); i != -1 {
		m.Tasks[i].Progress = progress
	}
	return m
}

// CancelTasks marks the pending tasks cancelled.
func (m *ActionLog) CancelTasks() *ActionLog {
	for i, t := range m.Tasks {
		if t.Status == Pending {
			m.Tasks[i].Status = Cancelled
		}
	}
	return m
}

// SetStatus sets the status of the log.
func (m *ActionLog) SetStatus(status Status) *ActionLog {
	m.Status = status
	return m
}

// AddModel records a record the action created, updated or deleted.
func (m *ActionLog) AddModel(modelType, id string, action ModelAction) *ActionLog {
	m.Models = append(m.Models, Model{ID: id, Type: modelType, Action: action})
	return m
}

// MarkComplete completes the log with an info message and saves it.
func (m *ActionLog) MarkComplete(msg string, args ...any) error {
	m.Status = Complete
	m.Progress = 100
	m.AddMessage(INFO, msg, args...)
	return m.Save()
}

// Save creates or updates the log; m becomes the stored record.
func (m *ActionLog) Save() error {
	var (
		saved *ActionLog
		err   error
	)
	if m.ID.Valid() {
		saved, err = Logs.Update(m)
	} else {
		saved, err = Logs.Create(m)
	}
	if err != nil {
		return err
	}
	*m = *saved
	return nil
}

// clone returns a copy of the log not sharing its slices, for a reader while the runner changes the log.
func (m *ActionLog) clone() *ActionLog {
	c := *m
	c.Tasks = append([]Task(nil), m.Tasks...)
	c.Messages = append([]logger.LogMessage(nil), m.Messages...)
	c.Models = append([]Model(nil), m.Models...)
	return &c
}
