package statuser

import (
	"context"
	"fmt"
	"slices"

	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

// NewMachine returns a machine changing the statuses of the owners stored by the repository. It panics when
// the statuses have an empty or duplicate ID, or a transition to a status not in them, like regexp.MustCompile.
func NewMachine[T Statuser](r repo.Repository[T], statuses ...Status[T]) *Machine[T] {
	ss := Statuses[T](statuses)
	for i, s := range ss {
		if !s.Valid() {
			panic(fmt.Sprintf("statuser: status %d has no ID", i))
		}
		if ss.FindIndex(s.ID) != i {
			panic(fmt.Sprintf("statuser: duplicate status %s", s.ID))
		}
		for _, to := range s.CanSwitchTo {
			if !ss.Contains(to) {
				panic(fmt.Sprintf("statuser: status %s switches to unknown status %s", s.ID, to))
			}
		}
	}
	return &Machine[T]{repo: r, statuses: ss}
}

// Machine changes the statuses of its owners by the transitions of the statuses.
type Machine[T Statuser] struct {
	repo     repo.Repository[T]
	statuses Statuses[T]
}

// FindStatus returns the status with the ID, or an invalid status
func (m *Machine[T]) FindStatus(status string) Status[T] {
	return m.statuses.Find(status)
}

// ChangeStatus switches the owner to the status with the ID of state, when its current status can switch to it.
// It calls the Enter function of the status, then updates the status column through the repository and records
// the change with the reason and the issuer in the same transaction, so both are written or neither is.
// On error the owner keeps its previous status. On success the Event of the status is published with a copy
// of the owner. It runs with context.Background(), see ChangeStatusWithContext.
func (m *Machine[T]) ChangeStatus(owner T, state Status[T], reason string, issuer *types.User) error {
	return m.ChangeStatusWithContext(context.Background(), owner, state, reason, issuer)
}

// ChangeStatusWithContext is like ChangeStatus, but runs the queries and the hooks with ctx.
func (m *Machine[T]) ChangeStatusWithContext(ctx context.Context, owner T, state Status[T], reason string, issuer *types.User) error {
	next := m.statuses.Find(state.ID)
	if !next.Valid() {
		return fmt.Errorf("unknown status %q", state.ID)
	}
	prevID := owner.GetStatus()
	cur := m.statuses.Find(prevID)
	if !cur.Valid() {
		return fmt.Errorf("invalid state transition from unknown status %q to %s", prevID, next.Name)
	}
	if !slices.Contains(cur.CanSwitchTo, next.ID) {
		return fmt.Errorf("invalid state transition from %s to %s", cur.Name, next.Name)
	}
	if next.Enter != nil {
		if err := next.Enter(owner, cur); err != nil {
			return err
		}
	}
	if err := owner.SetStatus(next.ID); err != nil {
		return err
	}
	// the record is inserted in the transaction of the update
	rec := newStatusRecord(owner, next.ID, reason, issuer)
	ctx = pg.WithTxHook(ctx, func(ctx context.Context, tx pg.Tx) error {
		return insertStatusRecord(ctx, tx, rec)
	})
	if _, err := m.repo.WithContext(ctx).Update(owner, "status"); err != nil {
		owner.SetStatus(prevID)
		return err
	}
	if next.Event != "" {
		events.System.PubDoc(next.Event, owner)
	}
	return nil
}

// Statuses returns the statuses without their functions
func (m *Machine[T]) Statuses() []ShortStatus {
	res := make([]ShortStatus, len(m.statuses))
	for i, v := range m.statuses {
		res[i] = v.Short()
	}
	return res
}

// Events returns the events published by the statuses
func (m *Machine[T]) Events() []string {
	var res []string
	for _, v := range m.statuses {
		if v.Event != "" {
			res = append(res, v.Event)
		}
	}
	return res
}
