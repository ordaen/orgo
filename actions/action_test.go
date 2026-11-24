package actions

import (
	"errors"
	"testing"
	"time"

	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskStatuses(m *ActionLog) []Status {
	var s []Status
	for _, t := range m.Tasks {
		s = append(s, t.Status)
	}
	return s
}

func TestActionRun(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	var ran []string
	a := New("Install", &types.User{Name: "John"}).WithOwner("1", "servers", "ns1")
	a.Task("a", "First", func() error {
		ran = append(ran, "a")
		assert.Equal(t, Running, Logs.FindByID(a.Log.ID).Tasks[0].Status, "saved running")
		a.Log.Info("from the task")
		return nil
	})
	a.Task("b", "Nothing to do", func() error { ran = append(ran, "b"); return ErrSkip })
	a.Task("c", "Last", func() error { ran = append(ran, "c"); return nil })
	require.NoError(t, a.Run())

	assert.Equal(t, []string{"a", "b", "c"}, ran)
	got := Logs.FindByID(a.Log.ID)
	assert.Equal(t, Complete, got.Status)
	assert.Equal(t, 100, got.Progress)
	assert.Equal(t, []Status{Complete, Skipped, Complete}, taskStatuses(got))
	assert.Equal(t, "John", got.Issuer.Name)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "from the task", got.Messages[0].Message)
}

func TestActionRunFailure(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	a := New("Install", nil)
	a.Task("a", "First", func() error { return nil })
	a.Task("b", "Failing", func() error { return errors.New("no route to host") })
	a.Task("c", "Never", func() error { t.Error("ran after a failure"); return nil })
	assert.EqualError(t, a.Run(), "no route to host")

	got := Logs.FindByID(a.Log.ID)
	assert.Equal(t, Failed, got.Status)
	assert.Equal(t, 33, got.Progress)
	assert.Equal(t, []Status{Complete, Failed, Cancelled}, taskStatuses(got))
	require.Len(t, got.Messages, 1)
	assert.Equal(t, ERROR, got.Messages[0].Level)
	assert.Equal(t, "no route to host", got.Messages[0].Message)
}

func TestActionRunPanic(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	a := New("Install", nil)
	a.Task("a", "Panicking", func() error { panic("boom") })
	a.Task("b", "Never", func() error { return nil })
	assert.EqualError(t, a.Run(), "panic: boom")

	got := Logs.FindByID(a.Log.ID)
	assert.Equal(t, Failed, got.Status)
	assert.Equal(t, []Status{Failed, Cancelled}, taskStatuses(got))
}

func TestActionStart(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	release := make(chan struct{})
	a := New("Install", nil).WithOwner("1", "servers", "ns1").WithRequest(map[string]string{"action": "x"})
	a.Task("a", "Blocked", func() error { <-release; return nil })
	started, err := a.Start()
	require.NoError(t, err)
	assert.True(t, started.ID.Valid())
	assert.Equal(t, Running, started.Status)

	// another exclusive action of the owner is refused while it runs, one of another owner begins
	other := New("Restart", nil).WithOwner("1", "servers", "ns1").Exclusive()
	other.Task("a", "Restart", func() error { return nil })
	assert.ErrorIs(t, other.Run(), ErrRunning)
	assert.ErrorContains(t, other.Run(), "Install", "names the running one")
	assert.False(t, other.Log.ID.Valid(), "no log")
	free := New("Restart", nil).WithOwner("2", "servers", "ns2").Exclusive()
	free.Task("a", "Restart", func() error { return nil })
	assert.NoError(t, free.Run())

	close(release)
	require.Eventually(t, func() bool { return Logs.FindByID(started.ID).Status == Complete }, 5*time.Second,
		10*time.Millisecond)
	assert.Equal(t, Pending, started.Tasks[0].Status, "the copy is not changed by the run")

	again := New("Restart", nil).WithOwner("1", "servers", "ns1").Exclusive()
	again.Task("a", "Restart", func() error { return nil })
	assert.NoError(t, again.Run(), "the first one is finished")
}

func TestFailUnfinished(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	running := &ActionLog{Name: "Install", Status: Running,
		Tasks: []Task{{ID: "a", Status: Complete}, {ID: "b", Status: Running}, {ID: "c", Status: Pending}}}
	require.NoError(t, running.Save())
	pending := &ActionLog{Name: "Restart"}
	require.NoError(t, pending.Save())
	done := &ActionLog{Name: "Done", Status: Complete}
	require.NoError(t, done.Save())

	n, err := FailUnfinished()
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	got := Logs.FindByID(running.ID)
	assert.Equal(t, Failed, got.Status)
	assert.Equal(t, []Status{Complete, Failed, Cancelled}, taskStatuses(got))
	assert.Equal(t, "interrupted: the application stopped", got.Messages[0].Message)
	assert.Equal(t, Failed, Logs.FindByID(pending.ID).Status)
	assert.Equal(t, Complete, Logs.FindByID(done.ID).Status)
}

func TestDeleteBefore(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	old := &ActionLog{Name: "Old", Status: Complete}
	require.NoError(t, old.Save())
	stuck := &ActionLog{Name: "Stuck", Status: Running}
	require.NoError(t, stuck.Save())
	_, err := pg.Exec(`UPDATE action_logs SET updated = now() - interval '2 days'`)
	require.NoError(t, err)
	recent := &ActionLog{Name: "Recent", Status: Failed}
	require.NoError(t, recent.Save())

	n, err := DeleteBefore(time.Now().Add(-24 * time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.False(t, Logs.FindByID(old.ID).ID.Valid())
	assert.True(t, Logs.FindByID(stuck.ID).ID.Valid(), "not finished")
	assert.True(t, Logs.FindByID(recent.ID).ID.Valid())
}

func TestActionKeepGoing(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	a := New("Update Stats", nil).KeepGoing()
	a.Task("ns1", "ns1", func() error { return errors.New("ns1 down") })
	a.Task("ns2", "ns2", func() error { return nil })
	a.Task("ns3", "ns3", func() error { return errors.New("ns3 down") })
	var ended error
	a.After(func(err error) { ended = err })
	assert.EqualError(t, a.Run(), "ns1 down\nns3 down")
	assert.EqualError(t, ended, "ns1 down\nns3 down", "After gets the error")

	got := Logs.FindByID(a.Log.ID)
	assert.Equal(t, Failed, got.Status)
	assert.Equal(t, 100, got.Progress, "every task ran")
	assert.Equal(t, []Status{Failed, Complete, Failed}, taskStatuses(got))
	assert.Len(t, got.Messages, 2)
}

func TestActionAfter(t *testing.T) {
	require.NoError(t, pg.ClearTables("action_logs"))

	done := make(chan error, 1)
	a := New("Install", nil)
	a.Task("a", "First", func() error { return nil })
	a.After(func(err error) {
		assert.Equal(t, Complete, Logs.FindByID(a.Log.ID).Status, "after the last save")
		done <- err
	})
	a.After(func(error) { panic("recovered") })
	_, err := a.Start()
	require.NoError(t, err)
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("After not called")
	}
}
