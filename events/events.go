// Package events publishes named events with data to the subscribers of hubs. The repositories created with
// repo.WithEvents publish their committed changes to the Creates, Updates and Deletes hubs, named by the table.
//
//	unsubscribe := events.Updates.Sub("users", func(e events.Event) {
//		user := e.Doc().(*User)
//		...
//	})
package events

import (
	"log/slog"
	"reflect"

	"github.com/ordaen/orgo/model"
)

// Model is model.Model, the models published by Hub.Pub.
type Model = model.Model

// All is the event name of the subscriptions receiving all events of a hub.
const All = "*"

// The hubs of the application events and of the record changes published by the repositories.
var (
	// System receives the application events.
	System = NewHub("SYSTEM")
	// Creates receives the created records.
	Creates = NewHub("CREATES")
	// Updates receives the updated records.
	Updates = NewHub("UPDATES")
	// Deletes receives the deleted records.
	Deletes = NewHub("DELETES")
)

// PubSub publishes events and subscribes to them, it is implemented by Hub.
type PubSub interface {
	Pub(m Model)
	PubEvent(name string)
	PubData(name string, data any)
	PubID(tableName string, id model.ModelID)
	Sub(name string, handler EventFunc, opts ...SubOption) (unsubscribe func())
	SubFunc(handler EventFunc, names ...string) (unsubscribe func())
}

var _ PubSub = (*Hub)(nil)

// EventFunc handles the events of a subscription.
type EventFunc func(Event)

// Event is a published event. The data is shared by all subscribers, so it must not be changed by them.
type Event struct {
	Name string
	Data any
}

// Doc returns the model published by Pub, or nil.
func (e Event) Doc() Model {
	if v, ok := e.Data.(Model); ok {
		return v
	}
	return nil
}

// ID returns the ID of the model published by Pub or the ID published by PubID, or "".
func (e Event) ID() string {
	switch v := e.Data.(type) {
	case Model:
		return v.GetID().String()
	case model.ModelID:
		return v.String()
	}
	return ""
}

// LogValue logs the event name and its ID or data type.
func (e Event) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("name", e.Name)}
	if id := e.ID(); id != "" {
		attrs = append(attrs, slog.String("id", id))
	}
	if e.Data != nil {
		attrs = append(attrs, slog.String("type", reflect.TypeOf(e.Data).String()))
	}
	return slog.GroupValue(attrs...)
}

// copyModel returns a shallow copy of the model when it is a pointer to a struct, so the publisher can change
// its model after publishing it while the subscribers read the copy.
func copyModel(m Model) Model {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return m
	}
	c := reflect.New(v.Elem().Type())
	c.Elem().Set(v.Elem())
	return c.Interface().(Model)
}
