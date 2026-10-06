package gql

import (
	"context"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/sessions"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userCtx returns a request context with a session of the user 7.
func userCtx(t *testing.T) context.Context {
	ctx, c := ginCtx(t, nil)
	s := &sessions.Session{}
	s.SetUserFields(&types.User{ID: model.ID(7), Name: "admin"})
	c.Set("session", s)
	return ctx
}

func countLogs(t *testing.T, where string, args ...any) int {
	n, err := pg.CountWhere("logs", where, args...)
	require.NoError(t, err)
	return n
}

func TestFindUpdate(t *testing.T) {
	seeded := seedItems(t, 1)
	ctx := userCtx(t)
	name := "renamed"

	rec, err := NewQuerier(items).FindUpdate(ctx, seeded[0].ID, func(rec *item, ch *Changes) error {
		UpdateIfExists(ch, "name", &rec.Name, &name)
		UpdateIfExists(ch, "active", &rec.Active, new(false))
		// not added to the changes, so not written
		rec.OwnerID = 99
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "renamed", rec.Name)
	assert.False(t, rec.Active)
	assert.Equal(t, model.ID(1), rec.OwnerID, "the record is returned as stored")

	stored := items.FindByID(seeded[0].ID)
	assert.Equal(t, "renamed", stored.Name)
	assert.False(t, stored.Active)
	assert.Equal(t, model.ID(1), stored.OwnerID, "only the changed columns are written")

	assert.Equal(t, 1, countLogs(t, "action = 'update' AND level = 'info' AND user_id = 7 AND owner_id = ?", seeded[0].ID.String()))
	var changes Changes
	require.NoError(t, pg.DB.QueryRow(context.Background(), `SELECT changes FROM logs WHERE action = 'update'`).Scan(&changes))
	assert.Equal(t, Changes{{Key: "name", From: "item-00", To: "renamed"}, {Key: "active", From: "true", To: "false"}}, changes)
}

func TestFindUpdateNoChanges(t *testing.T) {
	seeded := seedItems(t, 1)
	before := items.FindByID(seeded[0].ID)
	rec, err := NewQuerier(items).FindUpdate(userCtx(t), seeded[0].ID, func(rec *item, ch *Changes) error {
		UpdateIfExists(ch, "name", &rec.Name, nil)
		UpdateIfExists(ch, "name", &rec.Name, new("item-00"))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, seeded[0].ID, rec.ID)
	assert.Equal(t, before.Updated, items.FindByID(seeded[0].ID).Updated, "nothing is written")
	assert.Zero(t, countLogs(t, "TRUE"))
}

func TestFindUpdateErrors(t *testing.T) {
	seeded := seedItems(t, 2)
	ctx := userCtx(t)
	q := NewQuerier(items)

	_, err := q.FindUpdate(ctx, seeded[0].ID, func(rec *item, ch *Changes) error {
		UpdateIfExists(ch, "name", &rec.Name, new("x"))
		return assert.AnError
	})
	assert.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, "item-00", items.FindByID(seeded[0].ID).Name, "an error of update writes nothing")

	_, err = q.Where("owner_id = ?", 1).FindUpdate(ctx, seeded[1].ID, func(*item, *Changes) error {
		t.Error("update called for a record out of the scope")
		return nil
	})
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = q.FindUpdate(ctx, seeded[0].ID, func(rec *item, ch *Changes) error {
		ch.Add("not_a_column", "a", "b")
		return nil
	})
	assert.Error(t, err)
	assert.Equal(t, 1, countLogs(t, "action = 'update' AND level = 'error'"), "the failed update is logged")
}

func TestFindUpdateCached(t *testing.T) {
	seeded := seedItems(t, 1)
	cached := repo.NewCached(&item{}, nil)
	require.NoError(t, cached.Setup())
	assert.Equal(t, "item-00", cached.FindByID(seeded[0].ID).Name)

	_, err := NewQuerier(cached).FindUpdate(userCtx(t), seeded[0].ID, func(rec *item, ch *Changes) error {
		UpdateIfExists(ch, "name", &rec.Name, new("cached"))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "cached", cached.FindByID(seeded[0].ID).Name, "the cache sees the update")
}

func TestChangedColumns(t *testing.T) {
	ch := Changes{{Key: "data.name"}, {Key: "name"}, {Key: "data.age"}, {Key: "name"}}
	assert.Equal(t, []string{"data", "name"}, changedColumns(ch))
}
