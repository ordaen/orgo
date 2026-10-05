package logger

import (
	"encoding/json"

	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

var Logs = repo.Register(&logsStore{})

type logsStore struct {
	repo.Base[*Log]
}

const (
	INFO  LogLevel = "info"
	WARN  LogLevel = "warn"
	ERROR LogLevel = "error"
	DEBUG LogLevel = "debug"
)

type LogLevel string

// Log model
type Log struct {
	model.CreateOnly[model.ID]

	Level     LogLevel        `json:"lvl"`
	Source    string          `json:"source"`
	Action    string          `json:"action"`
	Message   string          `json:"message,omitempty"`
	Changes   changes.Changes `json:"changes,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	OwnerID   string          `json:"owner_id"`
	OwnerType string          `json:"owner_type"`

	types.UserFields
}

func (*Log) TableName() string { return "logs" }

func (m *Log) WithUser(user *types.User) *Log {
	m.SetUserFields(user)
	return m
}

func (m *Log) WithMessage(message string) *Log {
	m.Message = message
	return m
}

func (m *Log) WithSource(source string) *Log {
	m.Source = source
	return m
}

func (m *Log) WithChanges(changes changes.Changes) *Log {
	m.Changes = changes
	return m
}

func (m *Log) WithData(data any) *Log {
	m.Data, _ = json.Marshal(data)
	return m
}

func (m *Log) WithModel(doc model.Model) *Log {
	m.OwnerType = pg.ModelType(doc)
	m.OwnerID = doc.GetID().String()
	return m
}

func (m *Log) Create() error {
	_, err := Logs.Create(m)
	return err
}
