package actions

import (
	"errors"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionLogModel(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	issuer := &types.User{ID: model.ID(2), Name: "John Doe", Type: "admins"}
	m := (&ActionLog{Name: "Install"}).WithOwner("1", "servers", "ns1").WithIssuer(issuer).
		AddRequest(map[string]string{"action": "install"}).AddTask("a", "First").Info("a %s", "message")
	require.NoError(t, m.Save())
	assert.Equal(t, "ActionLog", pg.ModelType(m))

	got := Logs.FindByID(m.ID)
	require.True(t, got.ID.Valid())
	assert.Equal(t, Pending, got.Status, "the default")
	assert.Equal(t, "ns1", got.OwnerName)
	assert.Equal(t, issuer.Name, got.Issuer.Name)
	assert.JSONEq(t, `{"action":"install"}`, string(got.Data.Request))
	assert.Equal(t, []Task{{ID: "a", Name: "First", Status: Pending}}, got.Tasks)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "a message", got.Messages[0].Message)
	assert.Equal(t, INFO, got.Messages[0].Level)

	require.NoError(t, got.MarkComplete("done"))
	assert.Equal(t, Complete, Logs.FindByID(m.ID).Status)
	assert.Equal(t, 100, Logs.FindByID(m.ID).Progress)
}

func TestActionLogTasks(t *testing.T) {
	m := &ActionLog{}
	m.AddTask("1", "one").AddTask("2", "two").AddTask("3", "three").AddTask("1", "again")
	assert.Len(t, m.Tasks, 3, "an ID once")

	m.StartTask("1")
	assert.Equal(t, Running, m.Tasks[0].Status)
	m.EndTask("1", Complete)
	assert.Equal(t, 33, m.Progress)
	m.EndTask("2", Complete)
	assert.Equal(t, 66, m.Progress)
	m.TaskProgress("3", 40)
	assert.Equal(t, 40, m.Tasks[2].Progress)
	m.EndTaskNoProgress("3", Failed)
	assert.Equal(t, 66, m.Progress, "a failure keeps the progress")

	m.SetTasks(Task{ID: "x", Name: "X", Status: Complete}, Task{ID: "y", Name: "Y"})
	m.CancelTasks()
	assert.Equal(t, []Task{{ID: "x", Name: "X", Status: Cancelled}, {ID: "y", Name: "Y", Status: Cancelled}}, m.Tasks)

	m.Err(nil).Error("100%% done").Err(errors.New("100% failed"))
	assert.Equal(t, "100% done", m.Messages[0].Message, "a format")
	assert.Equal(t, "100% failed", m.Messages[1].Message, "an error as it is")
	m.AddMessage(INFO, "")
	assert.Len(t, m.Messages, 2, "an empty message is ignored")

	m.AddModel("zones", "5", Created)
	assert.Equal(t, []Model{{ID: "5", Type: "zones", Action: Created}}, m.Models)
}

func TestStatusFinished(t *testing.T) {
	assert.True(t, Complete.Finished())
	assert.True(t, Failed.Finished())
	assert.False(t, Running.Finished())
	assert.False(t, Pending.Finished())
}
