package statuser

import (
	"context"
	"fmt"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

// StatusRecords is the repository of the status changes, in status_records.
var StatusRecords = repo.Register(&statusRecords{})

type statusRecords struct {
	repo.Base[*StatusRecord]
}

// FindByOwner returns the status records of the model, the oldest first
func (r *statusRecords) FindByOwner(m model.Model) []*StatusRecord {
	return r.FindMany("owner_id = ? AND owner_type = ? ORDER BY created, id", m.GetID().String(), pg.ModelType(m))
}

// CreateStatusRecord records the change of the status of the model by the user
func CreateStatusRecord(m model.Model, status, reason string, user *types.User) error {
	_, err := pg.Insert(newStatusRecord(m, status, reason, user))
	return err
}

func newStatusRecord(m model.Model, status, reason string, user *types.User) *StatusRecord {
	return &StatusRecord{
		Status:    status,
		Reason:    reason,
		OwnerID:   m.GetID().String(),
		OwnerType: pg.ModelType(m),
		Issuer:    user,
	}
}

// insertStatusRecord inserts the record in the transaction.
func insertStatusRecord(ctx context.Context, tx pg.Tx, rec *StatusRecord) error {
	q, err := pg.BuildInsert(rec)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, q.SQL, q.Args...); err != nil {
		return fmt.Errorf("record status %s: %w", rec.Status, err)
	}
	return nil
}

// StatusRecord is a change of the status of the model OwnerType with OwnerID
type StatusRecord struct {
	model.CreateOnly[model.ID]
	Status    string      `json:"status"`
	Reason    string      `json:"reason"`
	OwnerID   string      `json:"owner_id"`
	OwnerType string      `json:"owner_type"`
	Issuer    *types.User `json:"issuer,omitempty"`
}

// TableName returns "status_records".
func (m *StatusRecord) TableName() string { return "status_records" }

// Short returns the record without its owner.
func (m *StatusRecord) Short() ShortStatusRecord {
	return ShortStatusRecord{Status: m.Status, Reason: m.Reason, Time: m.Created, User: m.Issuer}
}

// ShortStatusRecord is a StatusRecord without its owner, for the clients.
type ShortStatusRecord struct {
	Status string      `json:"status"`
	Reason string      `json:"reason"`
	Time   time.Time   `json:"time"`
	User   *types.User `json:"user"`
}
