package sessions

import (
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setExpires sets the session expiration in the database and the cache.
func setExpires(t *testing.T, s *Session, expires time.Time) {
	_, err := pg.DB.Exec(t.Context(), `UPDATE sessions SET expires = $2 WHERE id = $1`, s.ID, expires)
	require.NoError(t, err)
	require.NoError(t, Sessions.Setup())
}

func TestModelUserSession(t *testing.T) {
	clearTables(t, "sessions")
	s, err := Sessions.CreateSession(nil, "kind1", 10)
	require.NoError(t, err)
	assert.NotEmpty(t, s.ID)
	assert.NotEmpty(t, s.Token)
	assert.False(t, s.WithSSO())
	assert.WithinDuration(t, time.Now().Add(10*time.Hour), s.Expires, time.Minute)

	s1 := Sessions.Find(string(s.ID), "kind1")
	require.NotNil(t, s1)
	assert.Equal(t, s.ID, s1.ID)
	assert.Equal(t, s.Token, s1.Token)
	assert.Nil(t, Sessions.Find(string(s.ID), "kind2"))

	s2 := Sessions.FindByToken(s.Token, "kind1")
	require.NotNil(t, s2)
	assert.Equal(t, s.ID, s2.ID)

	require.NoError(t, s1.Delete())
	assert.Nil(t, Sessions.Find(string(s.ID), "kind1"))
}

func TestSessionSSO(t *testing.T) {
	clearTables(t, "sessions")
	user := &types.User{ID: model.ID(1), Type: "admins"}
	_, err := Sessions.CreateSessionSSO(user, "sso-token", nil)
	assert.Error(t, err)
	expires := time.Now().Add(time.Hour)
	_, err = Sessions.CreateSessionSSO(user, "", &expires)
	assert.Error(t, err)

	s, err := Sessions.CreateSessionSSO(user, "sso-token", &expires)
	require.NoError(t, err)
	assert.True(t, s.WithSSO())
	assert.Equal(t, "login", s.Type)
	assert.Equal(t, user.ID, s.UserID)

	found := Sessions.FindBySSO("sso-token")
	require.NotNil(t, found)
	assert.Equal(t, s.ID, found.ID)
}

// TestFindBySSOEmpty checks the sessions without SSO are not found by an empty SSO token.
func TestFindBySSOEmpty(t *testing.T) {
	clearTables(t, "sessions")
	_, err := Sessions.CreateSession(&types.User{ID: model.ID(1)}, "login", 1)
	require.NoError(t, err)
	assert.Nil(t, Sessions.FindBySSO(""))
}

func TestSessionsByUser(t *testing.T) {
	clearTables(t, "sessions")
	user := &types.User{ID: model.ID(1), Type: "admins", IP: "1.1.1.1"}
	other := &types.User{ID: model.ID(1), Type: "admins", IP: "2.2.2.2"}
	for range 2 {
		_, err := Sessions.CreateSession(user, "login", 1)
		require.NoError(t, err)
	}
	_, err := Sessions.CreateSession(other, "login", 1)
	require.NoError(t, err)

	s := Sessions.FindByUser(user)
	require.NotNil(t, s)
	assert.Equal(t, user.IP, s.UserIP)
	assert.Nil(t, Sessions.FindByUser(nil))
	assert.Nil(t, Sessions.FindByUser(&types.User{ID: model.ID(5)}))

	require.NoError(t, Sessions.DeleteByUser(user))
	require.NoError(t, Sessions.DeleteByUser(nil))
	assert.Nil(t, Sessions.FindByUser(user))
	assert.NotNil(t, Sessions.FindByUser(other))
	assert.Equal(t, 1, Sessions.Count())
}

func TestCheckSessionExpired(t *testing.T) {
	clearTables(t, "sessions")
	s, err := Sessions.CreateSession(nil, "login", 1)
	require.NoError(t, err)
	setExpires(t, s, time.Now().Add(-time.Minute))
	assert.Nil(t, Sessions.Find(string(s.ID), "login"))
	assert.Zero(t, Sessions.Count(), "the expired session is deleted")
}

func TestCheckSessionRenew(t *testing.T) {
	clearTables(t, "sessions")
	s, err := Sessions.CreateSession(nil, "login", 1)
	require.NoError(t, err)
	setExpires(t, s, time.Now().Add(10*time.Minute))

	found := Sessions.Find(string(s.ID), "login")
	require.NotNil(t, found)
	want := time.Now().Add(renewHours * time.Hour)
	assert.WithinDuration(t, want, found.Expires, time.Minute)
	assert.WithinDuration(t, want, Sessions.FindByID(s.ID).Expires, time.Minute, "the renewal is stored")

	// a session not expiring soon is not renewed
	setExpires(t, s, time.Now().Add(time.Hour))
	found = Sessions.Find(string(s.ID), "login")
	require.NotNil(t, found)
	assert.WithinDuration(t, time.Now().Add(time.Hour), found.Expires, time.Minute)
}

func TestSessionsDeleteExpired(t *testing.T) {
	clearTables(t, "sessions")
	expired, err := Sessions.CreateSession(nil, "login", 1)
	require.NoError(t, err)
	active, err := Sessions.CreateSession(nil, "login", 1)
	require.NoError(t, err)
	setExpires(t, expired, time.Now().Add(-time.Minute))

	require.NoError(t, Sessions.DeleteExpired())
	assert.Equal(t, 1, Sessions.Count())
	assert.Equal(t, active.ID, Sessions.FindByID(active.ID).ID)
}
