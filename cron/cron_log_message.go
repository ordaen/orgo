package cron

import (
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/repo"
)

var LogMessages = repo.Register(&cronLogMessages{})

type cronLogMessages struct {
	repo.Base[*CronLogMessage]
}

// CronLogMessage model
type CronLogMessage struct {
	model.CreateOnly[model.ID]

	LogID   model.ID `json:"log_id"`
	Message string   `json:"message"`
	Error   bool     `json:"error,omitempty"`
}

func (m *CronLogMessage) TableName() string { return "cron_log_messages" }
