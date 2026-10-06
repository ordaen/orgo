package gql

import (
	"errors"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/sessions"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeAndUpdateIfExists(t *testing.T) {
	data := []struct {
		old        string
		new        *string
		updated    string
		validators []func(string) error
		err        error
		changes    Changes
	}{
		// nill case should not do anything
		{
			old:     "initial value",
			new:     nil,
			updated: "initial value",
		},
		// same case should not do anything
		{
			old:     "initial value",
			new:     new("initial value"),
			updated: "initial value",
		},
		// simple case should update and log the change
		{
			old:     "initial value",
			new:     new("updated value"),
			updated: "updated value",
			changes: Changes{
				{Key: "F1", From: "initial value", To: "updated value"},
			},
		},
		// with validator that returns an error should not update and return the error
		{
			old:     "initial value",
			new:     new("updated value"),
			updated: "initial value",
			validators: []func(string) error{
				func(s string) error { return errors.New("error") },
			},
			err: errors.New("error"),
		},
		// with validator that returns no error should update and log the change
		{
			old:     "initial value",
			new:     new("updated value"),
			updated: "updated value",
			validators: []func(string) error{
				func(s string) error { return nil },
			},
			changes: Changes{
				{Key: "F1", From: "initial value", To: "updated value"},
			},
		},
		// should clear values
		{
			old:     "initial value",
			new:     new("updated\u3164\u3164\u3164\u3164\u3164 value\t\r\n"),
			updated: "updated value",
			changes: Changes{
				{Key: "F1", From: "initial value", To: "updated value"},
			},
		},
	}

	for _, v := range data {
		var changed Changes
		err := SanitizeAndUpdateIfExists(&changed, "F1", &v.old, v.new, v.validators...)
		require.Equal(t, v.err, err)
		require.Equal(t, v.changes, changed)
		require.Equal(t, v.updated, v.old)
	}
}

func TestUpdateIfExists(t *testing.T) {
	var changed Changes
	n := 1
	assert.False(t, UpdateIfExists(&changed, "n", &n, nil))
	assert.False(t, UpdateIfExists(&changed, "n", &n, new(1)))
	assert.True(t, UpdateIfExists(&changed, "n", &n, new(2)))
	assert.Equal(t, 2, n)
	assert.Equal(t, Changes{{Key: "n", From: "1", To: "2"}}, changed)
}

func TestSliceHelpers(t *testing.T) {
	var dst int
	assert.False(t, SetIfExists(&dst, nil))
	assert.True(t, SetIfExists(&dst, new(3)))
	assert.Equal(t, 3, dst)

	ptrs := SliceToPointerSlice([]int{1, 2})
	assert.Equal(t, []int{1, 2}, PointerSliceToSlice(ptrs))
	assert.Equal(t, 5, *Point(5))
}

func TestCreateLog(t *testing.T) {
	require.NoError(t, pg.ClearTables("logs"))
	ctx, c := ginCtx(t, nil)
	s := &sessions.Session{}
	s.SetUserFields(&types.User{ID: model.ID(7), Name: "admin"})
	c.Set("session", s)

	require.NoError(t, CreateLogInfo(ctx, "update", Changes{{Key: "name", From: "a", To: "b"}}))
	require.NoError(t, CreateLogError(ctx, "update", nil, assert.AnError))
	n, err := pg.CountWhere("logs", "user_id = 7 AND action = 'update'")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}
