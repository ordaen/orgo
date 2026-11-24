package actions

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
)

// ErrSkip is returned by a task function that had nothing to do: the task is skipped, the next ones run.
var ErrSkip = errors.New("actions: task skipped")

// ErrRunning is the error of an exclusive action begun while another action of its owner is not finished.
var ErrRunning = errors.New("another action of this record is running")

// Action runs the tasks of a log in order: each task is marked running, then complete, skipped or failed, and the
// log is saved after each change. The first failure cancels the next tasks and fails the log (KeepGoing runs them
// all); a panic fails it too. Build it with New, the With methods and Task, then call Run or Start once.
type Action struct {
	// Log is the log of the action. A task function may add messages to it (they are saved with the task's end,
	// or at once with Save); it belongs to the goroutine running the tasks.
	Log       *ActionLog
	funcs     []func() error
	exclusive bool
	keepGoing bool
	after     []func(err error)
}

// New returns an action named name, run by the issuer.
func New(name string, issuer *types.User) *Action {
	return &Action{Log: &ActionLog{Name: name, Issuer: issuer}}
}

// WithOwner sets the record the action is about.
func (a *Action) WithOwner(id, ownerType, name string) *Action {
	a.Log.WithOwner(id, ownerType, name)
	return a
}

// WithRequest stores the request of the action in the log. Mask its secrets first.
func (a *Action) WithRequest(req any) *Action {
	a.Log.AddRequest(req)
	return a
}

// Exclusive makes the action refuse to begin (ErrRunning) while a log of the same owner is pending or running.
func (a *Action) Exclusive() *Action {
	a.exclusive = true
	return a
}

// KeepGoing runs every task even after a failure (tasks independent of each other, one per server): the log fails
// at the end when a task failed, with the errors of all.
func (a *Action) KeepGoing() *Action {
	a.keepGoing = true
	return a
}

// After adds a function called when the action ends, after the last save of the log, with its error (nil when it
// completed), in the goroutine of the tasks.
func (a *Action) After(fn func(err error)) *Action {
	a.after = append(a.after, fn)
	return a
}

// Task adds a task: its ID, unique in the action, its name shown to the users, its function.
func (a *Action) Task(id, name string, fn func() error) *Action {
	a.Log.AddTask(id, name)
	a.funcs = append(a.funcs, fn)
	return a
}

// beginMu makes the check of an exclusive action and the save of its log one step.
var beginMu sync.Mutex

// begin saves the log, running.
func (a *Action) begin() error {
	beginMu.Lock()
	defer beginMu.Unlock()
	if a.exclusive && a.Log.OwnerType != "" {
		busy := Logs.FindWhere("owner_type = ? AND owner_id = ? AND status IN ?", a.Log.OwnerType, a.Log.OwnerID,
			pg.In([]string{string(Pending), string(Running)}))
		if busy.ID.Valid() {
			return fmt.Errorf("%w: %s", ErrRunning, busy.Name)
		}
	}
	a.Log.Status = Running
	return a.Log.Save()
}

// Run runs the action in the calling goroutine and returns the error of the failed task (KeepGoing: of all the
// failed ones), or of the log's first save (nothing runs then). The log is saved after each step: Log is the
// stored record.
func (a *Action) Run() error {
	if err := a.begin(); err != nil {
		return err
	}
	return a.runAll()
}

// Start saves the log, running, then runs the tasks in a goroutine. It returns a copy of the saved log (the
// running action keeps changing Log), or the error of the save.
func (a *Action) Start() (*ActionLog, error) {
	if err := a.begin(); err != nil {
		return nil, err
	}
	saved := a.Log.clone()
	go func() { _ = a.runAll() }()
	return saved, nil
}

// runAll runs the tasks, then the After functions.
func (a *Action) runAll() error {
	err := a.run()
	for _, fn := range a.after {
		func() {
			defer logs.Recover()
			fn(err)
		}()
	}
	return err
}

func (a *Action) run() (err error) {
	defer logs.Recover(func(r any) {
		err = fmt.Errorf("panic: %v", r)
		a.fail(err, "")
	})
	var failed []error
	for i, fn := range a.funcs {
		id := a.Log.Tasks[i].ID
		a.Log.StartTask(id)
		a.save()
		switch err := fn(); {
		case errors.Is(err, ErrSkip):
			a.Log.EndTask(id, Skipped)
		case err != nil && a.keepGoing:
			a.Log.Err(err).EndTask(id, Failed)
			failed = append(failed, err)
		case err != nil:
			a.fail(err, id)
			return err
		default:
			a.Log.EndTask(id, Complete)
		}
		a.save()
	}
	if len(failed) > 0 {
		a.Log.SetStatus(Failed)
		a.save()
		return errors.Join(failed...)
	}
	a.Log.Status = Complete
	a.Log.Progress = 100
	a.save()
	return nil
}

// fail fails the log with the error, the task id failed (none for a panic: the running one) and the next cancelled.
func (a *Action) fail(err error, id string) {
	for i, t := range a.Log.Tasks {
		if t.ID == id || (id == "" && t.Status == Running) {
			a.Log.Tasks[i].Status = Failed
		}
	}
	a.Log.Err(err).CancelTasks().SetStatus(Failed)
	a.save()
}

// save saves the log; a failed save is logged, the action goes on (its result does not depend on the log).
func (a *Action) save() {
	if err := a.Log.Save(); err != nil {
		logs.Error(err, "save the action log "+a.Log.Name)
	}
}

// FailUnfinished fails the logs left pending or running by a stopped application, whose goroutines are gone:
// their running tasks fail, the pending ones are cancelled. Call it at start, before any action begins.
func FailUnfinished() (int, error) {
	unfinished := Logs.FindMany("status IN ?", pg.In([]string{string(Pending), string(Running)}))
	for _, m := range unfinished {
		for i, t := range m.Tasks {
			if t.Status == Running {
				m.Tasks[i].Status = Failed
			}
		}
		m.Error("interrupted: the application stopped").CancelTasks().SetStatus(Failed)
		if err := m.Save(); err != nil {
			return 0, err
		}
	}
	return len(unfinished), nil
}

// DeleteBefore deletes the finished logs last updated before t and returns their count.
func DeleteBefore(t time.Time) (int64, error) {
	return pg.Exec(`DELETE FROM action_logs WHERE updated < $1 AND status NOT IN ('pending', 'running')`, t)
}
