// Package statuser changes the statuses of models by the transitions of a Machine and records the changes.
package statuser

import "github.com/ordaen/orgo/model"

// Statuser is a model with a status, changed by a Machine.
type Statuser interface {
	model.Model
	GetStatus() string
	SetStatus(state string) error
}

type WithStatuses interface {
	Statuses() []ShortStatus
}

// BaseStatus is embedded in the models with a status column
type BaseStatus struct {
	Status string `json:"status,readonly"`
}

func (b *BaseStatus) GetStatus() string {
	return b.Status
}

func (b *BaseStatus) SetStatus(status string) error {
	b.Status = status
	return nil
}
