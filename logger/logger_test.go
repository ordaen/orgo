package logger

import (
	"encoding/json"
	"testing"

	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testModel struct {
	model.Base[model.ID]
	Name string `json:"name"`
}

func (m *testModel) TableName() string {
	return "test_models"
}

func (m *testModel) ModelType() string {
	return "TestModel"
}

func TestCreate(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs"))
	mod := &testModel{ID: 1}
	data := map[string]string{"key": "val"}
	message := "msg"
	changes := changes.Changes{{Key: "field", From: "old", To: "new"}}
	user := &types.User{
		ID:   model.ID(2),
		Name: "John Doe",
		Type: "admins",
		IP:   "1.1.1.1",
	}
	NewLog(INFO, "source1", "action1").WithChanges(changes).WithData(data).WithModel(mod).WithUser(user).WithMessage(message).Create()
	count, err := pg.Count("logs")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	r, err := pg.Query(new(Log)).First()
	require.NoError(t, err)
	assert.Equal(t, INFO, r.Level)
	assert.Equal(t, "source1", r.Source)
	assert.Equal(t, "action1", r.Action)
	assert.Equal(t, mod.ID.String(), r.OwnerID)
	assert.Equal(t, mod.ModelType(), r.OwnerType)
	assert.Equal(t, user.ID, r.UserID)
	assert.Equal(t, user.Name, r.UserName)
	assert.Equal(t, user.Type, r.UserType)
	assert.Equal(t, user.IP, r.UserIP)
	assert.Equal(t, changes, r.Changes)
	assert.Equal(t, json.RawMessage(`{"key": "val"}`), r.Data)
}

func TestInfo(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs"))
	Info("action1").Create()
	count, err := pg.Count("logs")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	r, err := pg.Query(new(Log)).First()
	require.NoError(t, err)
	assert.Equal(t, INFO, r.Level)
	assert.Equal(t, "system", r.Source)
	assert.Equal(t, "action1", r.Action)
}

func TestWarn(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs"))
	Warn("action1").Create()
	count, err := pg.Count("logs")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	r, err := pg.Query(new(Log)).First()
	assert.NoError(t, err)
	assert.Equal(t, WARN, r.Level)
	assert.Equal(t, "system", r.Source)
	assert.Equal(t, "action1", r.Action)
}

func TestError(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs"))
	Error("action1").Create()
	count, err := pg.Count("logs")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	r, err := pg.Query(new(Log)).First()
	require.NoError(t, err)
	assert.Equal(t, ERROR, r.Level)
	assert.Equal(t, "system", r.Source)
	assert.Equal(t, "action1", r.Action)
}

func TestDebug(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs"))
	Debug("action1").Create()
	count, err := pg.Count("logs")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	r, err := pg.Query(new(Log)).First()
	require.NoError(t, err)
	assert.Equal(t, DEBUG, r.Level)
	assert.Equal(t, "system", r.Source)
	assert.Equal(t, "action1", r.Action)
}

func TestRecords(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs", "test_models"))
	user := &types.User{ID: model.ID(2), Name: "John Doe", Type: "admins"}

	m := &testModel{Name: "first"}
	require.NoError(t, CreateRecord(m, user))
	m, err := pg.Query(new(testModel)).First()
	require.NoError(t, err)

	m.Name = "second"
	ch := changes.Changes{{Key: "name", From: "first", To: "second"}}
	require.NoError(t, UpdateRecord(m, user, ch, "name"))
	require.NoError(t, DeleteRecord(m, user))

	logs, err := pg.Query(new(Log)).Order("id").Select()
	require.NoError(t, err)
	require.Len(t, logs, 3)
	for i, action := range []string{"create", "update", "delete"} {
		assert.Equal(t, INFO, logs[i].Level)
		assert.Equal(t, action, logs[i].Action)
		assert.Equal(t, "TestModel", logs[i].OwnerType)
		assert.Equal(t, m.ID.String(), logs[i].OwnerID)
		assert.Equal(t, user.ID, logs[i].UserID)
	}
	assert.Equal(t, ch, logs[1].Changes)
	assert.Contains(t, string(logs[2].Data), `"name": "second"`)
}

func TestRecordError(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs", "test_models"))
	err := DeleteRecord(&testModel{ID: 1}, nil)
	assert.ErrorIs(t, err, pg.ErrNoRowsAffected)

	r, err := pg.Query(new(Log)).First()
	require.NoError(t, err)
	assert.Equal(t, ERROR, r.Level)
	assert.Equal(t, "delete", r.Action)
	assert.NotEmpty(t, r.Message)
}
