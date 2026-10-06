// Package statuser changes the statuses of models by the transitions of a Machine and records the changes.
package statuser

import "github.com/ordaen/orgo/model"

// Statuser is a model with a status, changed by a Machine.
type Statuser interface {
	model.Model
	// GetStatus returns the ID of the status.
	GetStatus() string
	// SetStatus sets the ID of the status, it does not store it.
	SetStatus(state string) error
}

// WithStatuses lists its statuses, it is implemented by Machine.
type WithStatuses interface {
	// Statuses returns the statuses without their functions.
	Statuses() []ShortStatus
}

// BaseStatus is embedded in the models with a status column
type BaseStatus struct {
	Status string `json:"status,readonly"`
}

// GetStatus returns Status.
func (b *BaseStatus) GetStatus() string {
	return b.Status
}

// SetStatus sets Status, it does not store it.
func (b *BaseStatus) SetStatus(status string) error {
	b.Status = status
	return nil
}
