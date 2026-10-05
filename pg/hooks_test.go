package pg

import (
	"context"
	"errors"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hook behavior is configured globally, because AfterCreate is called on a new instance mapped from the database
var (
	hookCalls     []string
	hookBeforeErr error
	hookAfterErr  error
)

type hookModel struct {
	model.Base[model.ID]
	Name  string
	Email string
}

func (m *hookModel) TableName() string {
	return "base_models"
}

func (m *hookModel) BeforeCreate(ctx context.Context, tx Tx) error {
	hookCalls = append(hookCalls, "before:"+m.ID.String())
	if hookBeforeErr != nil {
		return hookBeforeErr
	}
	// changes are inserted
	m.Email = m.Name + "@example.com"
	return nil
}

func (m *hookModel) AfterCreate(ctx context.Context, tx Tx) error {
	hookCalls = append(hookCalls, "after:"+m.Email)
	if hookAfterErr != nil {
		return hookAfterErr
	}
	// runs in the create transaction and sees the inserted record
	var found int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM base_models WHERE id = $1", m.ID).Scan(&found); err != nil {
		return err
	}
	if found != 1 {
		return errors.New("inserted record is not visible in the hook transaction")
	}
	_, err := tx.Exec(ctx, "INSERT INTO hook_logs (message) VALUES ($1)", "created "+m.ID.String())
	return err
}

func setupHookTest(t *testing.T) {
	t.Helper()
	_, err := DB.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS hook_logs (
		"id" Bigserial PRIMARY KEY,
		"message" Text)`)
	require.NoError(t, err)
	require.NoError(t, ClearTables("base_models", "hook_logs"))
	hookCalls, hookBeforeErr, hookAfterErr = nil, nil, nil
	t.Cleanup(func() { hookCalls, hookBeforeErr, hookAfterErr = nil, nil, nil })
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	n, err := Count(table)
	require.NoError(t, err)
	return n
}

func TestCreateModelHooks(t *testing.T) {
	setupHookTest(t)

	created, err := CreateModel(&hookModel{Name: "john"})
	require.NoError(t, err)
	require.True(t, created.ID.Valid())
	assert.Equal(t, "john@example.com", created.Email, "BeforeCreate changes must be inserted")
	// BeforeCreate runs before the insert, AfterCreate on the inserted record
	assert.Equal(t, []string{"before:0", "after:john@example.com"}, hookCalls)

	var message string
	require.NoError(t, DB.QueryRow(context.Background(), "SELECT message FROM hook_logs").Scan(&message))
	assert.Equal(t, "created "+created.ID.String(), message, "AfterCreate writes must be committed")
}

func TestCreateModelBeforeHookError(t *testing.T) {
	setupHookTest(t)
	hookBeforeErr = errors.New("invalid model")

	_, err := CreateModel(&hookModel{Name: "john"})
	require.ErrorIs(t, err, hookBeforeErr)
	assert.EqualError(t, err, "invalid model")
	assert.Equal(t, []string{"before:0"}, hookCalls)
	assert.Zero(t, countRows(t, "base_models"))
}

func TestCreateModelAfterHookError(t *testing.T) {
	setupHookTest(t)
	hookAfterErr = errors.New("after failed")

	_, err := CreateModel(&hookModel{Name: "john"})
	require.ErrorIs(t, err, hookAfterErr)
	assert.EqualError(t, err, "after failed")
	assert.Equal(t, []string{"before:0", "after:john@example.com"}, hookCalls)
	assert.Zero(t, countRows(t, "base_models"), "insert must be rolled back when AfterCreate fails")
}

func TestCreateModelHooksNilModel(t *testing.T) {
	setupHookTest(t)

	_, err := CreateModel[*hookModel](nil)
	require.EqualError(t, err, "[PG] model *pg.hookModel is nil")
	assert.Empty(t, hookCalls, "hooks must not be called on a nil model")
}

func (m *hookModel) BeforeUpdate(ctx context.Context, tx Tx) error {
	hookCalls = append(hookCalls, "before-update:"+m.Name)
	if hookBeforeErr != nil {
		return hookBeforeErr
	}
	// changes are updated
	m.Email = m.Name + "@updated.example.com"
	return nil
}

func (m *hookModel) AfterUpdate(ctx context.Context, tx Tx) error {
	hookCalls = append(hookCalls, "after-update:"+m.Email)
	if hookAfterErr != nil {
		return hookAfterErr
	}
	// runs in the update transaction and sees the updated record
	var name string
	if err := tx.QueryRow(ctx, "SELECT name FROM base_models WHERE id = $1", m.ID).Scan(&name); err != nil {
		return err
	}
	if name != m.Name {
		return errors.New("updated record is not visible in the hook transaction")
	}
	_, err := tx.Exec(ctx, "INSERT INTO hook_logs (message) VALUES ($1)", "updated "+m.ID.String())
	return err
}

// createHookModel creates a record for update tests and resets the recorded hook calls.
func createHookModel(t *testing.T) *hookModel {
	t.Helper()
	created, err := CreateModel(&hookModel{Name: "john"})
	require.NoError(t, err)
	require.NoError(t, ClearTables("hook_logs"))
	hookCalls = nil
	return created
}

func findHookModel(t *testing.T, id model.ID) *hookModel {
	t.Helper()
	found, err := Query(&hookModel{}).Where("id = ?", id).First()
	require.NoError(t, err)
	return found
}

func TestUpdateModelHooks(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)

	created.Name = "jane"
	updated, err := UpdateModel(created)
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "jane", updated.Name)
	assert.Equal(t, "jane@updated.example.com", updated.Email, "BeforeUpdate changes must be updated")
	// BeforeUpdate runs on the given model, AfterUpdate on the updated record
	assert.Equal(t, []string{"before-update:jane", "after-update:jane@updated.example.com"}, hookCalls)

	var message string
	require.NoError(t, DB.QueryRow(context.Background(), "SELECT message FROM hook_logs").Scan(&message))
	assert.Equal(t, "updated "+created.ID.String(), message, "AfterUpdate writes must be committed")
}

func TestUpdateModelHooksWithFields(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)

	created.Name = "jane"
	updated, err := UpdateModel(created, "name")
	require.NoError(t, err)
	assert.Equal(t, "jane", updated.Name)
	// the email changed by BeforeUpdate is not in the fields, so it is not written
	assert.Equal(t, "john@example.com", updated.Email)
}

func TestUpdateModelBeforeHookError(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)
	hookBeforeErr = errors.New("invalid model")

	created.Name = "jane"
	_, err := UpdateModel(created)
	require.ErrorIs(t, err, hookBeforeErr)
	assert.EqualError(t, err, "invalid model")
	assert.Equal(t, []string{"before-update:jane"}, hookCalls)
	assert.Equal(t, "john", findHookModel(t, created.ID).Name)
}

func TestUpdateModelAfterHookError(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)
	hookAfterErr = errors.New("after failed")

	created.Name = "jane"
	_, err := UpdateModel(created)
	require.ErrorIs(t, err, hookAfterErr)
	assert.EqualError(t, err, "after failed")
	assert.Equal(t, []string{"before-update:jane", "after-update:jane@updated.example.com"}, hookCalls)
	assert.Equal(t, "john", findHookModel(t, created.ID).Name, "update must be rolled back when AfterUpdate fails")
	assert.Zero(t, countRows(t, "hook_logs"))
}

func TestUpdateModelInvalidNoHooks(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)

	// validation errors are returned before the transaction starts and hooks are called
	_, err := UpdateModel(&hookModel{Name: "jane"})
	assert.EqualError(t, err, `[PG] model hookModel has invalid id "0"`)
	_, err = UpdateModel(created, "missing")
	assert.EqualError(t, err, `[PG] model hookModel has no writable field "missing"`)
	_, err = UpdateModel[*hookModel](nil)
	assert.EqualError(t, err, "[PG] model *pg.hookModel is nil")
	assert.Empty(t, hookCalls)
}

func (m *hookModel) BeforeDelete(ctx context.Context, tx Tx) error {
	hookCalls = append(hookCalls, "before-delete:"+m.ID.String()+":"+m.Name)
	return hookBeforeErr
}

func (m *hookModel) AfterDelete(ctx context.Context, tx Tx) error {
	hookCalls = append(hookCalls, "after-delete:"+m.ID.String()+":"+m.Name)
	if hookAfterErr != nil {
		return hookAfterErr
	}
	// runs in the delete transaction, the record is already deleted there
	var found int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM base_models WHERE id = $1", m.ID).Scan(&found); err != nil {
		return err
	}
	if found != 0 {
		return errors.New("deleted record is still visible in the hook transaction")
	}
	_, err := tx.Exec(ctx, "INSERT INTO hook_logs (message) VALUES ($1)", "deleted "+m.ID.String())
	return err
}

func TestDeleteModelHooks(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)
	id := created.ID.String()

	// BeforeDelete runs on the given model, AfterDelete on the deleted record as it was stored
	_, err := DeleteModel(&hookModel{Base: model.Base[model.ID]{ID: created.ID}})
	require.NoError(t, err)
	assert.Equal(t, []string{"before-delete:" + id + ":", "after-delete:" + id + ":john"}, hookCalls)
	assert.Zero(t, countRows(t, "base_models"))

	var message string
	require.NoError(t, DB.QueryRow(context.Background(), "SELECT message FROM hook_logs").Scan(&message))
	assert.Equal(t, "deleted "+id, message, "AfterDelete writes must be committed")
}

func TestDeleteModelBeforeHookError(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)
	hookBeforeErr = errors.New("cannot delete")

	_, err := DeleteModel(created)
	require.ErrorIs(t, err, hookBeforeErr)
	assert.EqualError(t, err, "cannot delete")
	assert.Equal(t, []string{"before-delete:" + created.ID.String() + ":john"}, hookCalls)
	assert.Equal(t, 1, countRows(t, "base_models"))
}

func TestDeleteModelAfterHookError(t *testing.T) {
	setupHookTest(t)
	created := createHookModel(t)
	hookAfterErr = errors.New("after failed")

	_, err := DeleteModel(created)
	require.ErrorIs(t, err, hookAfterErr)
	assert.EqualError(t, err, "after failed")
	assert.Len(t, hookCalls, 2)
	assert.Equal(t, "john", findHookModel(t, created.ID).Name, "delete must be rolled back when AfterDelete fails")
	assert.Zero(t, countRows(t, "hook_logs"))
}

func TestDeleteModelInvalidNoHooks(t *testing.T) {
	setupHookTest(t)

	_, err := DeleteModel(&hookModel{})
	assert.EqualError(t, err, `[PG] model hookModel has invalid id "0"`)
	_, err = DeleteModel[*hookModel](nil)
	assert.EqualError(t, err, "[PG] model *pg.hookModel is nil")
	assert.Empty(t, hookCalls)

	// a missing record calls BeforeDelete, but not AfterDelete
	_, err = DeleteModel(&hookModel{Base: model.Base[model.ID]{ID: 999999}})
	require.ErrorIs(t, err, ErrNoRowsAffected)
	assert.Equal(t, []string{"before-delete:999999:"}, hookCalls)
}
