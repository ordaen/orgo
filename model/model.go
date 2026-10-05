// Package model defines the Model interface, the model IDs and the base structs embedded in models.
package model

import "time"

// Model is a record of a table. TableName returns the table, GetID the value of its id column.
// TableName must return the same table for every model of a type: it is read once per type, on a new model.
type Model interface {
	GetID() ModelID
	TableName() string
}

// Base is embedded in models with an ID and the Created and Updated timestamps, which are set by the database
// when the record is inserted and updated.
type Base[T ModelID] struct {
	ID      T         `json:"id,omitempty"`
	Created time.Time `json:"created" db:"created,created_at"`
	Updated time.Time `json:"updated" db:"updated,updated_at"`
}

// GetID returns the ID.
func (b *Base[T]) GetID() ModelID {
	return b.ID
}

// CreateOnly is embedded in models with an ID and the Created timestamp, which are inserted but never updated.
type CreateOnly[T ModelID] struct {
	ID      T         `json:"id,omitempty"`
	Created time.Time `json:"created" db:"created,created_at"`
}

// GetID returns the ID.
func (c *CreateOnly[T]) GetID() ModelID {
	return c.ID
}
