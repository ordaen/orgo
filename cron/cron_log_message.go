package cron

import (
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/repo"
)

// LogMessages is the repository of the messages of the job runs, in cron_log_messages.
var LogMessages = repo.Register(&cronLogMessages{})

type cronLogMessages struct {
	repo.Base[*CronLogMessage]
}

// CronLogMessage is a message of a job run, an error message when Error is set.
type CronLogMessage struct {
	model.CreateOnly[model.ID]

	LogID   model.ID `json:"log_id"`
	Message string   `json:"message"`
	Error   bool     `json:"error,omitempty"`
}

// TableName returns "cron_log_messages".
func (m *CronLogMessage) TableName() string { return "cron_log_messages" }
