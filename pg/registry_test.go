package pg

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSetupRepo records its setups in calls and fails them with err.
type fakeSetupRepo struct {
	name  string
	calls *[]string
	err   error
}

func (r *fakeSetupRepo) Setup() error {
	*r.calls = append(*r.calls, r.name)
	return r.err
}

// withRegistry restores the registered repositories, the init functions and the global DB connection
// when the test ends.
func withRegistry(t *testing.T) {
	t.Helper()
	repositories.mu.Lock()
	saved := repositories.repos
	repositories.mu.Unlock()
	inits.mu.Lock()
	savedInits := inits.funcs
	inits.mu.Unlock()
	t.Cleanup(func() {
		repositories.mu.Lock()
		repositories.repos = saved
		repositories.mu.Unlock()
		inits.mu.Lock()
		inits.funcs = savedInits
		inits.mu.Unlock()
		if !DB.Connected() {
			require.NoError(t, Connect(testConfig()))
		}
	})
}

func TestRegisterRepositorySetupOnConnect(t *testing.T) {
	withRegistry(t)
	var calls []string
	first := &fakeSetupRepo{name: "first", calls: &calls}
	assert.Same(t, first, RegisterRepository(first), "the repository is returned as it is")
	RegisterRepository(&fakeSetupRepo{name: "second", calls: &calls})

	// registering does not set up the repository, connecting does, in the registration order
	assert.Empty(t, calls)
	Close()
	require.NoError(t, Connect(testConfig()))
	assert.Equal(t, []string{"first", "second"}, calls)

	// every connect sets the repositories up again
	Close()
	require.NoError(t, Connect(testConfig()))
	assert.Equal(t, []string{"first", "second", "first", "second"}, calls)
}

func TestRegisterRepositorySetupError(t *testing.T) {
	withRegistry(t)
	var calls []string
	setupErr := errors.New("setup failed")
	failing := RegisterRepository(&fakeSetupRepo{name: "failing", calls: &calls, err: setupErr})
	RegisterRepository(&fakeSetupRepo{name: "next", calls: &calls})

	// a failed setup stops the setup and leaves DB unset, so Connect can be retried
	Close()
	err := Connect(testConfig())
	require.ErrorIs(t, err, setupErr)
	assertRegisteredAt(t, err, "setup of repository *pg.fakeSetupRepo", "TestRegisterRepositorySetupError")
	assert.False(t, DB.Connected())
	assert.Equal(t, []string{"failing"}, calls)

	failing.err = nil
	require.NoError(t, Connect(testConfig()))
	assert.True(t, DB.Connected())
	assert.Equal(t, []string{"failing", "failing", "next"}, calls)
}

func TestRegisterInitFuncRunsAfterSetup(t *testing.T) {
	withRegistry(t)
	var calls []string
	RegisterInitFunc(func() error {
		calls = append(calls, "init")
		return nil
	})
	RegisterRepository(&fakeSetupRepo{name: "repo", calls: &calls})

	// the init functions run on every connect, after the repositories registered before or after them
	assert.Empty(t, calls)
	Close()
	require.NoError(t, Connect(testConfig()))
	assert.Equal(t, []string{"repo", "init"}, calls)

	Close()
	require.NoError(t, Connect(testConfig()))
	assert.Equal(t, []string{"repo", "init", "repo", "init"}, calls)
}

func TestRegisterInitFuncError(t *testing.T) {
	withRegistry(t)
	var calls []string
	initErr := errors.New("init failed")
	fail := true
	RegisterInitFunc(func() error {
		calls = append(calls, "failing")
		if fail {
			return initErr
		}
		return nil
	})
	RegisterInitFunc(func() error {
		calls = append(calls, "next")
		return nil
	})

	// a failed init stops the inits and leaves DB unset, so Connect can be retried
	Close()
	err := Connect(testConfig())
	require.ErrorIs(t, err, initErr)
	assertRegisteredAt(t, err, "init function", "TestRegisterInitFuncError")
	assert.False(t, DB.Connected())
	assert.Equal(t, []string{"failing"}, calls)

	fail = false
	require.NoError(t, Connect(testConfig()))
	assert.True(t, DB.Connected())
	assert.Equal(t, []string{"failing", "failing", "next"}, calls)
}

// assertRegisteredAt asserts that err is a RegistrationError of what, registered by the test function in
// registry_test.go, with a message having the registration stack and no runtime frames.
func assertRegisteredAt(t *testing.T, err error, what, testFunc string) {
	t.Helper()
	var regErr *RegistrationError
	require.ErrorAs(t, err, &regErr)
	assert.Equal(t, what, regErr.What)

	stack := regErr.Stack()
	require.NotEmpty(t, stack)
	assert.Equal(t, "github.com/ordaen/orgo/pg."+testFunc, stack[0].Function)
	assert.True(t, strings.HasSuffix(stack[0].File, "/registry_test.go"), stack[0].File)
	for _, f := range stack {
		assert.False(t, strings.HasPrefix(f.Function, "runtime."), f.Function)
	}

	msg := err.Error()
	assert.True(t, strings.HasPrefix(msg, "[PG] "+what+" failed: "+regErr.Err.Error()+"\nregistered at:\n"), msg)
	assert.Contains(t, msg, testFunc)
	assert.Contains(t, msg, "registry_test.go:")
}
