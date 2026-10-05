package cron

import (
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCronRecord(t *testing.T) {
	clearTables(t)
	r := &CronRecord{Handler: "handler", Name: t.Name()}
	assert.Equal(t, "cron_records", r.TableName())
	assert.Equal(t, "CronRecord", pg.ModelType(r))
	r, err := Records.Create(r)
	require.NoError(t, err)
	assert.Equal(t, "handler", r.Handler)
	assert.Equal(t, r.ID, Records.FindByHandler("handler").ID)
}

func TestModelCronLog(t *testing.T) {
	clearTables(t)
	l := NewLog(t.Name(), "handler")
	assert.Equal(t, "cron_logs", l.TableName())
	assert.Equal(t, "CronLog", pg.ModelType(l))
	require.NoError(t, l.Create())
	assert.True(t, l.ID.Valid(), "Create sets the ID")
	assert.Equal(t, "running", Logs.FindByID(l.ID).Status)

	l.UpdateMessage("progress %d%%", 50)
	l.AddMessage("plain 100%")
	l.AddErrorString("failed %s", "step")
	l.AddError(nil)
	l.SetComplete(assert.AnError)
	stored := Logs.FindByID(l.ID)
	assert.Equal(t, "progress 50%", stored.Message)
	assert.Equal(t, "complete", stored.Status)

	msgs := LogMessages.FindMany("log_id = ? ORDER BY id", l.ID)
	require.Len(t, msgs, 3)
	assert.Equal(t, "plain 100%", msgs[0].Message)
	assert.False(t, msgs[0].Error)
	assert.Equal(t, "failed step", msgs[1].Message)
	assert.True(t, msgs[1].Error)
	assert.Equal(t, assert.AnError.Error(), msgs[2].Message)

	require.NoError(t, l.Protect())
	assert.True(t, Logs.FindByID(l.ID).Protected)
	require.NoError(t, l.Unprotect())
	assert.False(t, Logs.FindByID(l.ID).Protected)
}

func TestModelLogMessage(t *testing.T) {
	clearTables(t)
	r := &CronLogMessage{LogID: 123, Message: "msg", Error: true}
	assert.Equal(t, "cron_log_messages", r.TableName())
	assert.Equal(t, "CronLogMessage", pg.ModelType(r))
	r, err := LogMessages.Create(r)
	require.NoError(t, err)
	assert.Equal(t, model.ID(123), r.LogID)
	assert.Equal(t, "msg", r.Message)
	assert.True(t, r.Error)
}

// TestDeleteLogDeletesMessages checks the AfterDelete hook deletes the messages of the log.
func TestDeleteLogDeletesMessages(t *testing.T) {
	clearTables(t)
	l := NewLog("n", "handler")
	require.NoError(t, l.Create())
	l.AddMessage("m")
	require.NoError(t, Logs.Delete(l))
	assert.Zero(t, LogMessages.Count())
}

func TestCronLogger(t *testing.T) {
	clearTables(t)
	var logger CronLogger
	logger.LogInfo("ignored without log")
	l := NewLog("n", "handler")
	require.NoError(t, l.Create())
	logger.SetLog(l)
	assert.Same(t, l, logger.Log())
	logger.LogInfo("info %d", 1)
	logger.LogError(assert.AnError)
	logger.LogErrorString("err %d", 2)
	assert.Equal(t, 3, LogMessages.CountWhere("log_id = ?", l.ID))
}
