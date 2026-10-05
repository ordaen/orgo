package pg

import (
	"context"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctxKey struct{}

// ctxHookCalls records the ctx value seen by each hook of ctxModel
var ctxHookCalls []string

// ctxCancel is called by BeforeCreate when set, to cancel the ctx in the middle of the operation
var ctxCancel context.CancelFunc

type ctxModel struct {
	model.Base[model.ID]
	Name string
}

func (m *ctxModel) TableName() string {
	return "base_models"
}

func recordCtx(ctx context.Context, hook string) {
	v, _ := ctx.Value(ctxKey{}).(string)
	ctxHookCalls = append(ctxHookCalls, hook+":"+v)
}

func (m *ctxModel) BeforeCreate(ctx context.Context, tx Tx) error {
	recordCtx(ctx, "before-create")
	if ctxCancel != nil {
		ctxCancel()
	}
	return nil
}

func (m *ctxModel) AfterCreate(ctx context.Context, tx Tx) error {
	recordCtx(ctx, "after-create")
	return nil
}

func (m *ctxModel) BeforeUpdate(ctx context.Context, tx Tx) error {
	recordCtx(ctx, "before-update")
	return nil
}

func (m *ctxModel) AfterUpdate(ctx context.Context, tx Tx) error {
	recordCtx(ctx, "after-update")
	return nil
}

func (m *ctxModel) BeforeDelete(ctx context.Context, tx Tx) error {
	recordCtx(ctx, "before-delete")
	return nil
}

func (m *ctxModel) AfterDelete(ctx context.Context, tx Tx) error {
	recordCtx(ctx, "after-delete")
	return nil
}

func setupCtxTest(t *testing.T) {
	t.Helper()
	require.NoError(t, ClearTables("base_models"))
	ctxHookCalls, ctxCancel = nil, nil
	t.Cleanup(func() { ctxHookCalls, ctxCancel = nil, nil })
}

func TestModelWithContextPassesContextToHooks(t *testing.T) {
	setupCtxTest(t)
	ctx := context.WithValue(context.Background(), ctxKey{}, "request-1")

	created, err := CreateModelWithContext(ctx, &ctxModel{Name: "john"})
	require.NoError(t, err)

	created.Name = "jane"
	updated, err := UpdateModelWithContext(ctx, created, "name")
	require.NoError(t, err)
	assert.Equal(t, "jane", updated.Name)

	_, err = DeleteModelWithContext(ctx, updated)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"before-create:request-1", "after-create:request-1",
		"before-update:request-1", "after-update:request-1",
		"before-delete:request-1", "after-delete:request-1",
	}, ctxHookCalls)
	assert.Zero(t, countRows(t, "base_models"))
}

func TestModelWithContextCanceled(t *testing.T) {
	setupCtxTest(t)
	created, err := CreateModel(&ctxModel{Name: "john"})
	require.NoError(t, err)
	ctxHookCalls = nil

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = CreateModelWithContext(ctx, &ctxModel{Name: "jane"})
	require.ErrorIs(t, err, context.Canceled)

	created.Name = "jane"
	_, err = UpdateModelWithContext(ctx, created)
	require.ErrorIs(t, err, context.Canceled)

	_, err = DeleteModelWithContext(ctx, created)
	require.ErrorIs(t, err, context.Canceled)

	// nothing is written and no hooks are called when the transaction cannot start
	assert.Empty(t, ctxHookCalls)
	assert.Equal(t, 1, countRows(t, "base_models"))
	assert.Equal(t, "john", findCtxModel(t, created.ID).Name)
}

func TestCreateModelWithContextCanceledInHook(t *testing.T) {
	setupCtxTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctxCancel = cancel

	// the insert runs with the ctx canceled by BeforeCreate, so it fails and nothing is written
	_, err := CreateModelWithContext(ctx, &ctxModel{Name: "john"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"before-create:"}, ctxHookCalls)
	assert.Zero(t, countRows(t, "base_models"))
}

func findCtxModel(t *testing.T, id model.ID) *ctxModel {
	t.Helper()
	found, err := Query(&ctxModel{}).Where("id = ?", id).First()
	require.NoError(t, err)
	return found
}

func TestModelWithContextCanceledRollsBack(t *testing.T) {
	setupCtxTest(t)
	logs := captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	ctxCancel = cancel

	// the context is canceled in the middle of the operation, the transaction is still rolled back
	_, err := CreateModelWithContext(ctx, &ctxModel{Name: "john"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, countRows(t, "base_models"))

	for _, r := range logs() {
		assert.NotEqual(t, "rollback", r.SQL, "the rollback must not fail: %s", r.Error)
	}
}
