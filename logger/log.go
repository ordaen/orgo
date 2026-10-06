package logger

import (
	"encoding/json"

	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

// Logs is the repository of the logs, in the logs table.
var Logs = repo.Register(&logsStore{})

type logsStore struct {
	repo.Base[*Log]
}

// The log levels.
const (
	INFO  LogLevel = "info"
	WARN  LogLevel = "warn"
	ERROR LogLevel = "error"
	DEBUG LogLevel = "debug"
)

// LogLevel is the level of a log: INFO, WARN, ERROR or DEBUG.
type LogLevel string

// Log is an audit log: the Action from the Source by the user, with its changes or data, about the record
// OwnerType with OwnerID. Its With methods set its fields and return it, Create stores it.
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

// TableName returns "logs".
func (*Log) TableName() string { return "logs" }

// WithUser sets the user fields of the log to the user, a nil user leaves them unchanged.
func (m *Log) WithUser(user *types.User) *Log {
	m.SetUserFields(user)
	return m
}

// WithMessage sets the message of the log.
func (m *Log) WithMessage(message string) *Log {
	m.Message = message
	return m
}

// WithSource sets the source of the log.
func (m *Log) WithSource(source string) *Log {
	m.Source = source
	return m
}

// WithChanges sets the changes of the log.
func (m *Log) WithChanges(changes changes.Changes) *Log {
	m.Changes = changes
	return m
}

// WithData sets the data of the log to data encoded as JSON, no data when it cannot be encoded.
func (m *Log) WithData(data any) *Log {
	m.Data, _ = json.Marshal(data)
	return m
}

// WithModel sets the owner of the log to the model, its type and ID.
func (m *Log) WithModel(doc model.Model) *Log {
	m.OwnerType = pg.ModelType(doc)
	m.OwnerID = doc.GetID().String()
	return m
}

// Create stores the log.
func (m *Log) Create() error {
	_, err := Logs.Create(m)
	return err
}
