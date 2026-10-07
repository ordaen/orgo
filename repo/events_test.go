package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/pg/pgtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type repoEvents struct {
	creates, updates, deletes <-chan events.Event
}

// subscribeEvents subscribes to all events of the global hubs until the test ends.
func subscribeEvents(t *testing.T) repoEvents {
	t.Helper()
	sub := func(h *events.Hub) <-chan events.Event {
		ch := make(chan events.Event, 100)
		t.Cleanup(h.Sub(events.All, func(e events.Event) { ch <- e }))
		return ch
	}
	return repoEvents{creates: sub(events.Creates), updates: sub(events.Updates), deletes: sub(events.Deletes)}
}

// eventOf is the name and the ID of an event.
type eventOf struct {
	Name, ID string
}

func receiveEvent(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("event not received")
		return events.Event{}
	}
}

// assertNoEvents checks that none of the channels received an event after the published events were handled.
// It must be called in a synctest bubble.
func (e repoEvents) assertNoEvents(t *testing.T) {
	t.Helper()
	synctest.Wait()
	select {
	case ev := <-e.creates:
		t.Fatalf("unexpected create event: %+v", ev)
	case ev := <-e.updates:
		t.Fatalf("unexpected update event: %+v", ev)
	case ev := <-e.deletes:
		t.Fatalf("unexpected delete event: %+v", ev)
	default:
	}
}

func TestBaseWithEvents(t *testing.T) {
	pgtest.Synctest(t, func(t *testing.T) {
		require.NoError(t, pg.ClearTables("base_models"))
		evs := subscribeEvents(t)
		repo := New(&baseModel{}, WithEvents())

		created, err := repo.Create(&baseModel{Name: "john"})
		require.NoError(t, err)
		want := eventOf{Name: "baseModel", ID: created.ID.String()}
		e := receiveEvent(t, evs.creates)
		assert.Equal(t, want, eventOf{e.Name, e.ID()})
		doc, ok := e.Doc().(*baseModel)
		require.True(t, ok)
		assert.Equal(t, created, doc)
		assert.NotSame(t, created, doc, "the subscribers get a copy")

		created.Name = "jane"
		_, err = repo.Update(created)
		require.NoError(t, err)
		e = receiveEvent(t, evs.updates)
		assert.Equal(t, want, eventOf{e.Name, e.ID()})
		assert.Equal(t, "jane", e.Doc().(*baseModel).Name)

		err = repo.Delete(created)
		require.NoError(t, err)
		e = receiveEvent(t, evs.deletes)
		assert.Equal(t, want, eventOf{e.Name, e.ID()})
		assert.Equal(t, "jane", e.Doc().(*baseModel).Name, "the deleted record")

		// finds do not publish
		_ = repo.FindMany("")
		evs.assertNoEvents(t)
	})
}

func TestBaseWithoutEvents(t *testing.T) {
	pgtest.Synctest(t, func(t *testing.T) {
		require.NoError(t, pg.ClearTables("base_models"))
		evs := subscribeEvents(t)
		repo := New(&baseModel{})

		created, err := repo.Create(&baseModel{Name: "john"})
		require.NoError(t, err)
		_, err = repo.Update(created)
		require.NoError(t, err)
		err = repo.Delete(created)
		require.NoError(t, err)
		evs.assertNoEvents(t)
	})
}

func TestBaseEventsNotPublishedOnError(t *testing.T) {
	pgtest.Synctest(t, func(t *testing.T) {
		setupHookTest(t)
		evs := subscribeEvents(t)
		repo := New(&hookModel{}, WithEvents())

		// a failing hook rolls the change back, so nothing is published
		hookAfterErr = errors.New("after failed")
		_, err := repo.Create(&hookModel{Name: "john"})
		require.ErrorIs(t, err, hookAfterErr)
		hookAfterErr = nil

		created, err := repo.Create(&hookModel{Name: "john"})
		require.NoError(t, err)
		receiveEvent(t, evs.creates)

		hookBeforeErr = errors.New("before failed")
		_, err = repo.Update(created)
		require.ErrorIs(t, err, hookBeforeErr)
		err = repo.Delete(created)
		require.ErrorIs(t, err, hookBeforeErr)
		hookBeforeErr = nil

		// validation errors and missing records are not published
		_, err = repo.Update(&hookModel{})
		require.Error(t, err)
		err = repo.Delete(&hookModel{})
		require.Error(t, err)
		err = repo.Delete(created)
		require.NoError(t, err)
		receiveEvent(t, evs.deletes)
		err = repo.Delete(created)
		require.ErrorIs(t, err, pg.ErrNoRowsAffected)

		evs.assertNoEvents(t)
	})
}

func TestBaseWithContextKeepsEvents(t *testing.T) {
	require.NoError(t, pg.ClearTables("base_models"))
	evs := subscribeEvents(t)
	repo := New(&baseModel{}, WithEvents()).WithContext(context.Background())

	created, err := repo.Create(&baseModel{Name: "john"})
	require.NoError(t, err)
	assert.Equal(t, created.ID.String(), receiveEvent(t, evs.creates).ID())
}

func TestCachedWithEvents(t *testing.T) {
	pgtest.Synctest(t, func(t *testing.T) {
		require.NoError(t, pg.ClearTables("base_models"))
		evs := subscribeEvents(t)
		repo := NewCached(&baseModel{}, nil, WithEvents())

		created, err := repo.Create(&baseModel{Name: "john"})
		require.NoError(t, err)
		want := eventOf{Name: "baseModel", ID: created.ID.String()}
		e := receiveEvent(t, evs.creates)
		assert.Equal(t, want, eventOf{e.Name, e.ID()})

		_, err = repo.Update(created)
		require.NoError(t, err)
		e = receiveEvent(t, evs.updates)
		assert.Equal(t, want, eventOf{e.Name, e.ID()})

		err = repo.WithContext(context.Background()).Delete(created)
		require.NoError(t, err)
		e = receiveEvent(t, evs.deletes)
		assert.Equal(t, want, eventOf{e.Name, e.ID()})

		// a cache repository without the option does not publish
		plain := NewCached(&baseModel{}, nil)
		_, err = plain.Create(&baseModel{Name: "jane"})
		require.NoError(t, err)
		evs.assertNoEvents(t)
	})
}

// uniqueModel turns the unique violations of its writes into application errors.
type uniqueModel struct {
	model.Base[model.ID]
	Name string
}

func (m *uniqueModel) TableName() string {
	return "unique_models"
}

func (m *uniqueModel) HandleDBError(op string, err *pg.PgError) error {
	if err.Code == pg.UniqueViolation {
		return fmt.Errorf("%s: name %s already exists", op, m.Name)
	}
	return nil
}

func TestDBErrorHandlerNotPublished(t *testing.T) {
	pgtest.Synctest(t, func(t *testing.T) {
		ctx := context.Background()
		_, err := pg.DB.Exec(ctx, `DROP TABLE IF EXISTS unique_models; CREATE TABLE unique_models (
			"id" Bigserial PRIMARY KEY,
			"name" Text UNIQUE,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now())`)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pg.DB.Exec(ctx, `DROP TABLE unique_models`) })

		repo := New(&uniqueModel{}, WithEvents())
		_, err = repo.Create(&uniqueModel{Name: "john"})
		require.NoError(t, err)

		evs := subscribeEvents(t)
		_, err = repo.Create(&uniqueModel{Name: "john"})
		assert.EqualError(t, err, "create: name john already exists")
		evs.assertNoEvents(t)
	})
}
