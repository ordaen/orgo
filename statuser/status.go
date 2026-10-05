package statuser

import "slices"

// StatusEnterFunc is called before the owner enters the status from the prev status, an error stops the change.
type StatusEnterFunc[T Statuser] func(owner T, prev Status[T]) error

type ShortStatus struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Info        string   `json:"info,omitempty"`
	CanSwitchTo []string `json:"can_switch_to,omitempty"`
}

// Status of a Machine. The owner can switch from it to the statuses CanSwitchTo. Event is published
// to events.System with the owner when it enters the status.
type Status[T Statuser] struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Info        string             `json:"info,omitempty"`
	Event       string             `json:"event,omitempty"`
	CanSwitchTo []string           `json:"can_switch_to,omitempty"`
	Enter       StatusEnterFunc[T] `json:"-"`
}

func (s Status[T]) Valid() bool {
	return s.ID != ""
}

func (s Status[T]) Short() ShortStatus {
	return ShortStatus{ID: s.ID, Name: s.Name, Info: s.Info, CanSwitchTo: slices.Clone(s.CanSwitchTo)}
}

type Statuses[T Statuser] []Status[T]

// Find returns the status with the id, or an invalid status
func (s Statuses[T]) Find(id string) Status[T] {
	if i := s.FindIndex(id); i >= 0 {
		return s[i]
	}
	return Status[T]{}
}

// FindIndex returns the index of the status with the id, or -1
func (s Statuses[T]) FindIndex(id string) int {
	return slices.IndexFunc(s, func(state Status[T]) bool { return state.ID == id })
}

func (s Statuses[T]) Contains(id string) bool {
	return s.FindIndex(id) >= 0
}
