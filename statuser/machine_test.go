package statuser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type order struct {
	model.Base[model.ID]
	BaseStatus
}

func (o *order) TableName() string { return "orders" }

type document struct {
	model.Base[model.UUID]
	BaseStatus
}

func (d *document) TableName() string { return "documents" }

var errNotPaid = errors.New("not paid")

// orderStatuses are new -> paid -> shipped, new -> canceled, paid -> canceled. Entering shipped fails
// when shipFails is set.
func orderStatuses(shipFails *bool) []Status[*order] {
	return []Status[*order]{
		{ID: "new", Name: "New", CanSwitchTo: []string{"paid", "canceled"}},
		{ID: "paid", Name: "Paid", Event: "order.paid", CanSwitchTo: []string{"shipped", "canceled"}},
		{ID: "shipped", Name: "Shipped", Info: "final", Enter: func(o *order, prev Status[*order]) error {
			if *shipFails {
				return errNotPaid
			}
			return nil
		}},
		{ID: "canceled", Name: "Canceled"},
	}
}

func setupMachine(t *testing.T, r repo.Repository[*order]) (*Machine[*order], *bool) {
	t.Helper()
	require.NoError(t, pg.ClearTables("orders", "status_records"))
	shipFails := new(bool)
	return NewMachine(r, orderStatuses(shipFails)...), shipFails
}

func newOrder(t *testing.T, r repo.Repository[*order]) *order {
	t.Helper()
	o := &order{}
	o.Status = "new"
	o, err := r.Create(o)
	require.NoError(t, err)
	return o
}

func TestChangeStatus(t *testing.T) {
	orders := repo.New(&order{})
	m, _ := setupMachine(t, orders)
	o := newOrder(t, orders)
	paidEvents := make(chan events.Event, 1)
	t.Cleanup(events.System.Sub("order.paid", func(e events.Event) { paidEvents <- e }))

	user := &types.User{ID: model.ID(1), Name: "admin"}
	require.NoError(t, m.ChangeStatus(o, m.FindStatus("paid"), "paid by card", user))
	assert.Equal(t, "paid", o.Status)
	assert.Equal(t, "paid", orders.FindByID(o.ID).Status, "the status is stored")

	select {
	case e := <-paidEvents:
		doc := e.Doc().(*order)
		assert.Equal(t, o.ID, doc.ID)
		assert.Equal(t, "paid", doc.Status)
		assert.NotSame(t, o, doc, "the event has a copy of the owner")
	case <-time.After(time.Second):
		t.Fatal("status event not received")
	}

	recs := StatusRecords.FindByOwner(o)
	require.Len(t, recs, 1)
	assert.Equal(t, "paid", recs[0].Status)
	assert.Equal(t, "paid by card", recs[0].Reason)
	assert.Equal(t, "order", recs[0].OwnerType)
	assert.Equal(t, user, recs[0].Issuer)
}

func TestChangeStatusInvalidTransition(t *testing.T) {
	orders := repo.New(&order{})
	m, _ := setupMachine(t, orders)
	o := newOrder(t, orders)

	assert.EqualError(t, m.ChangeStatus(o, m.FindStatus("shipped"), "", nil), "invalid state transition from New to Shipped")
	assert.EqualError(t, m.ChangeStatus(o, Status[*order]{ID: "lost"}, "", nil), `unknown status "lost"`)
	assert.EqualError(t, m.ChangeStatus(o, m.FindStatus("missing"), "", nil), `unknown status ""`)

	o.Status = "unknown"
	assert.EqualError(t, m.ChangeStatus(o, m.FindStatus("paid"), "", nil), `invalid state transition from unknown status "unknown" to Paid`)
	assert.Empty(t, StatusRecords.FindByOwner(o))
}

// TestChangeStatusUsesMachineStatus checks the status of the machine is entered, not the given copy.
func TestChangeStatusUsesMachineStatus(t *testing.T) {
	orders := repo.New(&order{})
	m, shipFails := setupMachine(t, orders)
	o := newOrder(t, orders)
	require.NoError(t, m.ChangeStatus(o, m.FindStatus("paid"), "", nil))
	*shipFails = true
	assert.ErrorIs(t, m.ChangeStatus(o, Status[*order]{ID: "shipped"}, "", nil), errNotPaid)
}

func TestChangeStatusEnterFails(t *testing.T) {
	orders := repo.New(&order{})
	m, shipFails := setupMachine(t, orders)
	o := newOrder(t, orders)
	require.NoError(t, m.ChangeStatus(o, m.FindStatus("paid"), "", nil))

	*shipFails = true
	assert.ErrorIs(t, m.ChangeStatus(o, m.FindStatus("shipped"), "", nil), errNotPaid)
	assert.Equal(t, "paid", o.Status)
	assert.Equal(t, "paid", orders.FindByID(o.ID).Status)
	assert.Len(t, StatusRecords.FindByOwner(o), 1)
}

// TestChangeStatusUpdateFails checks the owner keeps its status when it can not be stored.
func TestChangeStatusUpdateFails(t *testing.T) {
	orders := repo.New(&order{})
	m, _ := setupMachine(t, orders)
	o := &order{ID: 999}
	o.Status = "new"

	assert.ErrorIs(t, m.ChangeStatus(o, m.FindStatus("paid"), "", nil), pg.ErrNoRowsAffected)
	assert.Equal(t, "new", o.Status)
	assert.Empty(t, StatusRecords.FindByOwner(o))
}

// TestChangeStatusCached checks the status is changed through the repository, so its cache sees it.
func TestChangeStatusCached(t *testing.T) {
	orders := repo.NewCached(&order{}, nil)
	m, _ := setupMachine(t, orders)
	o := newOrder(t, orders)
	assert.Equal(t, "new", orders.FindByID(o.ID).Status)

	require.NoError(t, m.ChangeStatus(o, m.FindStatus("canceled"), "", nil))
	assert.Equal(t, "canceled", orders.FindByID(o.ID).Status)
}

func TestChangeStatusUUIDOwner(t *testing.T) {
	require.NoError(t, pg.ClearTables("documents", "status_records"))
	docs := repo.New(&document{})
	m := NewMachine(docs,
		Status[*document]{ID: "draft", Name: "Draft", CanSwitchTo: []string{"published"}},
		Status[*document]{ID: "published", Name: "Published"},
	)
	d := &document{ID: "6f1c2a52-6a45-4a6b-9b55-2b1a3f7f0c12"}
	d.Status = "draft"
	d, err := docs.Create(d)
	require.NoError(t, err)

	require.NoError(t, m.ChangeStatus(d, m.FindStatus("published"), "", nil))
	recs := StatusRecords.FindByOwner(d)
	require.Len(t, recs, 1)
	assert.Equal(t, string(d.ID), recs[0].OwnerID)
}

func TestNewMachineInvalid(t *testing.T) {
	r := repo.New(&order{})
	assert.PanicsWithValue(t, "statuser: status 1 has no ID", func() {
		NewMachine(r, Status[*order]{ID: "a"}, Status[*order]{})
	})
	assert.PanicsWithValue(t, "statuser: duplicate status a", func() {
		NewMachine(r, Status[*order]{ID: "a"}, Status[*order]{ID: "a"})
	})
	assert.PanicsWithValue(t, "statuser: status a switches to unknown status b", func() {
		NewMachine(r, Status[*order]{ID: "a", CanSwitchTo: []string{"b"}})
	})
}

func TestMachineStatuses(t *testing.T) {
	m := NewMachine(repo.New(&order{}), orderStatuses(new(bool))...)
	short := m.Statuses()
	require.Len(t, short, 4)
	assert.Equal(t, ShortStatus{ID: "shipped", Name: "Shipped", Info: "final"}, short[2])
	short[0].CanSwitchTo[0] = "changed"
	assert.Equal(t, "paid", m.FindStatus("new").CanSwitchTo[0], "Statuses returns copies")
	assert.Equal(t, []string{"order.paid"}, m.Events())
	assert.False(t, m.FindStatus("missing").Valid())
}

// TestChangeStatusRecordFails checks the status is not changed when its record can not be written:
// the update and the record are written in one transaction.
func TestChangeStatusRecordFails(t *testing.T) {
	orders := repo.NewCached(&order{}, nil)
	m, _ := setupMachine(t, orders)
	o := newOrder(t, orders)
	_, err := pg.DB.Exec(t.Context(), `ALTER TABLE status_records ADD CONSTRAINT reason_ok CHECK (reason <> 'rejected')`)
	require.NoError(t, err)
	t.Cleanup(func() { pg.DB.Exec(context.Background(), `ALTER TABLE status_records DROP CONSTRAINT IF EXISTS reason_ok`) })
	paidEvents := make(chan events.Event, 1)
	t.Cleanup(events.System.Sub("order.paid", func(e events.Event) { paidEvents <- e }))

	err = m.ChangeStatus(o, m.FindStatus("paid"), "rejected", nil)
	assert.ErrorContains(t, err, "record status paid")
	assert.Equal(t, "new", o.Status)
	assert.Equal(t, "new", orders.FindByID(o.ID).Status, "the cache has the stored status")
	var stored string
	require.NoError(t, pg.DB.QueryRow(t.Context(), `SELECT status FROM orders WHERE id = $1`, o.ID).Scan(&stored))
	assert.Equal(t, "new", stored, "the update is rolled back")
	assert.Empty(t, StatusRecords.FindByOwner(o))
	select {
	case e := <-paidEvents:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}

	// the next change succeeds
	require.NoError(t, m.ChangeStatus(o, m.FindStatus("paid"), "ok", nil))
	assert.Len(t, StatusRecords.FindByOwner(o), 1)
}

func TestChangeStatusWithContext(t *testing.T) {
	orders := repo.New(&order{})
	m, _ := setupMachine(t, orders)
	o := newOrder(t, orders)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, m.ChangeStatusWithContext(ctx, o, m.FindStatus("paid"), "", nil), context.Canceled)
	assert.Equal(t, "new", o.Status)
	assert.Empty(t, StatusRecords.FindByOwner(o))
}
