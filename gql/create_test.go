package gql

import (
	"context"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	require.NoError(t, pg.ClearTables("items", "logs"))
	var name *string = new("created")
	var missing *bool

	rec, err := NewQuerier(items).Create(userCtx(t), func(rec *item) error {
		SetIfExists(&rec.Name, name)
		SetIfExists(&rec.Active, missing)
		rec.OwnerID = 3
		return nil
	})
	require.NoError(t, err)
	assert.True(t, rec.ID.Valid(), "the record is returned as stored")
	assert.Equal(t, "created", rec.Name)
	assert.False(t, rec.Active)

	stored := items.FindByID(rec.ID)
	assert.Equal(t, "created", stored.Name)
	assert.Equal(t, model.ID(3), stored.OwnerID)
	assert.Equal(t, 1, countLogs(t, "action = 'create' AND level = 'info' AND user_id = 7 AND owner_id = ?", rec.ID.String()))
}

func TestCreateErrors(t *testing.T) {
	require.NoError(t, pg.ClearTables("items", "logs"))
	_, err := NewQuerier(items).Create(userCtx(t), func(rec *item) error {
		rec.Name = "rejected"
		return assert.AnError
	})
	assert.ErrorIs(t, err, assert.AnError)
	assert.Zero(t, items.Count(), "an error of create writes nothing")
	assert.Zero(t, countLogs(t, "TRUE"))

	ctx, cancel := context.WithCancel(userCtx(t))
	cancel()
	_, err = NewQuerier(items).Create(ctx, func(rec *item) error { return nil })
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, items.Count())
	assert.Equal(t, 1, countLogs(t, "action = 'create' AND level = 'error'"), "the failed creation is logged")
}

func TestCreateCached(t *testing.T) {
	require.NoError(t, pg.ClearTables("items", "logs"))
	cached := repo.NewCached(&item{}, nil)
	require.NoError(t, cached.Setup())
	rec, err := NewQuerier(cached).Create(userCtx(t), func(rec *item) error {
		rec.Name = "cached"
		return nil
	})
	require.NoError(t, err)
	found, err := cached.FindCached(func(i *item) bool { return i.Name == "cached" })
	require.NoError(t, err)
	assert.Equal(t, rec.ID, found.ID, "the cache sees the new record")
}
