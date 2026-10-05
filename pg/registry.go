package pg

import (
	"fmt"
	"runtime"
	"sync"
)

// SetupRepository is a repository that loads its records when the global DB is connected, like repo.Cached.
type SetupRepository interface {
	Setup() error
}

// registered is a registered repository or init function with the stack of its registration.
type registered[T any] struct {
	v   T
	pcs []uintptr
}

// newRegistered returns v with the stack of the caller of the register function calling newRegistered.
func newRegistered[T any](v T) registered[T] {
	pcs := make([]uintptr, maxRegistrationFrames)
	n := runtime.Callers(3, pcs) // skip runtime.Callers, newRegistered and the register function
	return registered[T]{v: v, pcs: pcs[:n]}
}

// repositories are the registered repositories, set up in the registration order.
var repositories struct {
	mu    sync.Mutex
	repos []registered[SetupRepository]
}

// RegisterRepository adds r to the repositories set up by ConnectWithContext and returns r,
// so it can be registered where it is declared:
//
//	var Admins = pg.RegisterRepository(repo.NewCached(&Admin{}, nil))
//
// The repositories are set up on every connect, after the registered schema is migrated.
// A repository registered while DB is already connected is set up on the next connect.
// A failed setup is returned as a *RegistrationError with the stack of the RegisterRepository call.
func RegisterRepository[T SetupRepository](r T) T {
	reg := newRegistered[SetupRepository](r)
	repositories.mu.Lock()
	repositories.repos = append(repositories.repos, reg)
	repositories.mu.Unlock()
	return r
}

// setupRepositories sets up the registered repositories, stopping at the first error.
func setupRepositories() error {
	repositories.mu.Lock()
	repos := append([]registered[SetupRepository](nil), repositories.repos...)
	repositories.mu.Unlock()

	for _, r := range repos {
		if err := r.v.Setup(); err != nil {
			return &RegistrationError{What: fmt.Sprintf("setup of repository %T", r.v), Err: err, pcs: r.pcs}
		}
	}
	return nil
}

// InitFunc is a function run when the global DB is connected, after the registered repositories are set up.
type InitFunc func() error

// inits are the registered init functions, run in the registration order.
var inits struct {
	mu    sync.Mutex
	funcs []registered[InitFunc]
}

// RegisterInitFunc adds f to the functions run by ConnectWithContext.
// The functions run on every connect, after the registered repositories are set up.
// A function registered while DB is already connected runs on the next connect.
// A failed function is returned as a *RegistrationError with the stack of the RegisterInitFunc call.
func RegisterInitFunc(f InitFunc) {
	reg := newRegistered(f)
	inits.mu.Lock()
	inits.funcs = append(inits.funcs, reg)
	inits.mu.Unlock()
}

// runInits runs the registered init functions, stopping at the first error.
func runInits() error {
	inits.mu.Lock()
	funcs := append([]registered[InitFunc](nil), inits.funcs...)
	inits.mu.Unlock()

	for _, f := range funcs {
		if err := f.v(); err != nil {
			return &RegistrationError{What: "init function", Err: err, pcs: f.pcs}
		}
	}
	return nil
}
