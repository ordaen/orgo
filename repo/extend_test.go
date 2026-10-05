package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// the repositories are declared like in an application package

type user struct {
	model.Base[model.ID]
	Name  string
	Email string
}

func (u *user) TableName() string {
	return "base_models"
}

type userRepository struct {
	repo.Base[*user]
}

func (r *userRepository) FindByEmail(email string) *user {
	return r.FindWhere("email = ?", email)
}

type userCachedRepository struct {
	repo.Cached[*user]
}

func (r *userCachedRepository) FindByEmail(email string) (*user, error) {
	return r.FindCached(func(u *user) bool { return u.Email == email })
}

var (
	users       = repo.Register(&userRepository{}, repo.WithEvents())
	cachedUsers = repo.RegisterCached(&userCachedRepository{})
)

var (
	_ repo.Repository[*user] = users
	_ repo.Repository[*user] = cachedUsers
)

func seedUsers(t *testing.T, names ...string) []*user {
	t.Helper()
	require.NoError(t, pg.ClearTables("base_models"))
	res := make([]*user, 0, len(names))
	for _, name := range names {
		u, err := pg.CreateModel(&user{Name: name, Email: name + "@example.com"})
		require.NoError(t, err)
		res = append(res, u)
	}
	return res
}

// reconnect closes and connects the global DB, so the registered repositories are set up again.
func reconnect(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if !pg.DB.Connected() {
			require.NoError(t, pg.Connect(repo.TestConfig()))
		}
	})
	pg.Close()
	require.NoError(t, pg.Connect(repo.TestConfig()))
}

func TestRegister(t *testing.T) {
	seed := seedUsers(t, "john", "jane")
	assert.Equal(t, "base_models", users.TableName())

	assert.Equal(t, seed[1], users.FindByEmail("jane@example.com"))
	assert.Equal(t, &user{}, users.FindByEmail("nobody@example.com"))

	// the options are applied
	ch := make(chan events.Event, 1)
	t.Cleanup(events.Creates.Sub(func(e events.Event) { ch <- e }))
	created, err := users.Create(&user{Name: "jack"})
	require.NoError(t, err)
	select {
	case e := <-ch:
		assert.Equal(t, events.Event{TableName: "base_models", ID: created.ID.String()}, e)
	case <-time.After(time.Second):
		t.Fatal("create event not received")
	}
}

func TestRegisterWithContext(t *testing.T) {
	seed := seedUsers(t, "john")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// the copy keeps the repository type and its methods
	canceled := repo.WithContext(users, ctx)
	assert.Equal(t, &user{}, canceled.FindByEmail("john@example.com"))
	_, err := canceled.Query().Select()
	require.ErrorIs(t, err, context.Canceled)

	// the repository itself is not changed
	assert.Equal(t, seed[0], users.FindByEmail("john@example.com"))
}

func TestRegisterCached(t *testing.T) {
	seed := seedUsers(t, "john", "jane")

	// the repository is registered, so its cache is loaded on connect
	reconnect(t)
	_, err := pg.DB.Exec(context.Background(), "DELETE FROM base_models")
	require.NoError(t, err)

	found, err := cachedUsers.FindByEmail("jane@example.com")
	require.NoError(t, err)
	assert.Equal(t, seed[1], found)
	assert.Equal(t, seed[0], cachedUsers.FindByID(seed[0].ID))
	_, err = cachedUsers.FindByEmail("nobody@example.com")
	require.ErrorIs(t, err, pg.ErrRecordNotFound)

	// FindWhere still queries the database
	assert.Equal(t, &user{}, cachedUsers.FindWhere("email = ?", "jane@example.com"))
}

func TestRegisterCachedFindCached(t *testing.T) {
	seedUsers(t)
	require.NoError(t, cachedUsers.Setup())

	// writes change the cache the custom methods read
	john, err := cachedUsers.Create(&user{Name: "john", Email: "john@example.com"})
	require.NoError(t, err)
	jane, err := cachedUsers.Create(&user{Name: "jane", Email: "jane@example.com"})
	require.NoError(t, err)
	found, err := cachedUsers.FindByEmail("john@example.com")
	require.NoError(t, err)
	assert.Equal(t, john, found)

	// the returned record is a copy
	found.Email = "changed@example.com"
	_, err = cachedUsers.FindByEmail("changed@example.com")
	require.ErrorIs(t, err, pg.ErrRecordNotFound)

	jane.Email = "jane@new.example.com"
	_, err = cachedUsers.Update(jane)
	require.NoError(t, err)
	_, err = cachedUsers.FindByEmail("jane@example.com")
	require.ErrorIs(t, err, pg.ErrRecordNotFound)
	_, err = cachedUsers.FindByEmail("jane@new.example.com")
	require.NoError(t, err)

	all := cachedUsers.FindManyCached(func(*user) bool { return true })
	assert.ElementsMatch(t, []string{"john", "jane"}, []string{all[0].Name, all[1].Name})
	assert.Equal(t, []*user{}, cachedUsers.FindManyCached(func(u *user) bool { return u.Name == "nobody" }))

	require.NoError(t, cachedUsers.Delete(john))
	_, err = cachedUsers.FindByEmail("john@example.com")
	require.ErrorIs(t, err, pg.ErrRecordNotFound)
}

func TestRegisterCachedWithContext(t *testing.T) {
	seed := seedUsers(t, "john")
	require.NoError(t, cachedUsers.Setup())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// the copy shares the cache, so the cached methods work, the database queries use ctx
	canceled := repo.WithContext(cachedUsers, ctx)
	found, err := canceled.FindByEmail("john@example.com")
	require.NoError(t, err)
	assert.Equal(t, seed[0], found)
	assert.Empty(t, canceled.FindMany(""))
	require.ErrorIs(t, canceled.Setup(), context.Canceled)

	assert.Equal(t, seed, cachedUsers.FindMany(""))
}

func TestRegisterWithCachedType(t *testing.T) {
	seed := seedUsers(t, "john")

	// Register initializes the cache too, it is filled by FindByID and writes
	r := repo.Register(&userCachedRepository{})
	_, err := r.FindByEmail("john@example.com")
	require.ErrorIs(t, err, pg.ErrRecordNotFound)
	assert.Equal(t, seed[0], r.FindByID(seed[0].ID))
	found, err := r.FindByEmail("john@example.com")
	require.NoError(t, err)
	assert.Equal(t, seed[0], found)
}
