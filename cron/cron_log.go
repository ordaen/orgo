package cron

import (
	"context"
	"fmt"
	"strings"

	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
)

var (
	_ pg.AfterDeleteHook = (*CronLog)(nil)
)

// Logs is the repository of the job runs, in cron_logs.
var Logs = repo.Register(&cronLogs{})

type cronLogs struct {
	repo.Base[*CronLog]
}

// removeOldRecords deletes the not protected logs of the handler, except the newest KeepLogRecords ones,
// with their messages in one statement.
func removeOldRecords(handler string) error {
	keep := keepLogRecords()
	if keep <= 0 {
		return nil
	}
	_, err := pg.Exec(`WITH old AS (
			SELECT id FROM cron_logs WHERE handler = $1 AND protected IS NOT TRUE ORDER BY id DESC OFFSET $2
		), messages AS (
			DELETE FROM cron_log_messages WHERE log_id IN (SELECT id FROM old)
		)
		DELETE FROM cron_logs WHERE id IN (SELECT id FROM old)`, handler, keep)
	return err
}

// NewLog returns a new running log of the job with the name and the handler, it is not stored yet, see Create.
func NewLog(name, handler string) *CronLog {
	l := &CronLog{Handler: handler}
	l.Name = name
	l.SetRunning()
	return l
}

// CronLog is a run of a job with its status and message, its messages are CronLogMessage records.
// A protected log is not removed with the old logs, see CronSettings.
type CronLog struct {
	model.Base[model.ID]

	Handler   string `json:"handler"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Protected bool   `json:"protected,omitempty"`
	Failures  int    `json:"failures,omitempty"`
}

// TableName returns "cron_logs".
func (m *CronLog) TableName() string { return "cron_logs" }

// SetRunning sets the status of the log running, it does not store it.
func (m *CronLog) SetRunning() {
	m.Status = "running"
}

func (m *CronLog) addMessage(message string, isErr bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	msg := CronLogMessage{LogID: m.ID, Message: message, Error: isErr}
	if _, err := LogMessages.Create(&msg); err != nil {
		logs.Error(err, "cron log message:")
	}
}

// UpdateMessage sets and stores the message of the log
func (m *CronLog) UpdateMessage(message string, args ...any) {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	m.Message = message
	if _, err := Logs.Update(m, "message"); err != nil {
		logs.Error(err, "cron log update message:")
	}
}

// AddMessage adds a message to the log, formatted with the args when they are given
func (m *CronLog) AddMessage(message string, args ...any) {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	m.addMessage(message, false)
}

// AddErrorString adds an error message to the log, formatted with the args when they are given
func (m *CronLog) AddErrorString(message string, args ...any) {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	m.addMessage(message, true)
}

// AddError adds the error to the log
func (m *CronLog) AddError(err error) {
	if err != nil {
		m.addMessage(err.Error(), true)
	}
}

// SetComplete adds the error and stores the log complete
func (m *CronLog) SetComplete(err error) {
	if err != nil {
		m.AddError(err)
	}
	m.Status = "complete"
	if _, err := Logs.Update(m, "status"); err != nil {
		logs.Error(err, "cron log complete:")
	}
}

// Save stores the log
func (m *CronLog) Save() error {
	_, err := Logs.Update(m)
	return err
}

// Create stores the new log and sets its ID, then removes the old logs of its handler in the background
func (m *CronLog) Create() error {
	created, err := Logs.Create(m)
	if err != nil {
		return err
	}
	*m = *created
	go func() {
		if err := removeOldRecords(m.Handler); err != nil {
			logs.Error(err, "cron remove old logs:")
		}
	}()
	return nil
}

// Protect stores the log protected, it is not removed with the old logs.
func (m *CronLog) Protect() error {
	m.Protected = true
	_, err := Logs.Update(m, "protected")
	return err
}

// Unprotect stores the log not protected, it can be removed with the old logs.
func (m *CronLog) Unprotect() error {
	m.Protected = false
	_, err := Logs.Update(m, "protected")
	return err
}

// AfterDelete deletes the messages of the deleted log
func (m *CronLog) AfterDelete(ctx context.Context, tx pg.Tx) error {
	_, err := tx.Exec(ctx, "DELETE FROM cron_log_messages WHERE log_id = $1", m.ID)
	return err
}

// CronLogger is embedded in the types logging to the log of a job run. Its methods do nothing without a log.
type CronLogger struct {
	log *CronLog
}

// SetLog sets the log of the run.
func (l *CronLogger) SetLog(log *CronLog) {
	l.log = log
}

// Log returns the log of the run, or nil.
func (l *CronLogger) Log() *CronLog {
	return l.log
}

// LogInfo adds a message to the log, see CronLog.AddMessage.
func (l *CronLogger) LogInfo(message string, args ...any) {
	if l.log != nil {
		l.log.AddMessage(message, args...)
	}
}

// LogError adds the error to the log, see CronLog.AddError.
func (l *CronLogger) LogError(err error) {
	if l.log != nil {
		l.log.AddError(err)
	}
}

// LogErrorString adds an error message to the log, see CronLog.AddErrorString.
func (l *CronLogger) LogErrorString(message string, args ...any) {
	if l.log != nil {
		l.log.AddErrorString(message, args...)
	}
}
