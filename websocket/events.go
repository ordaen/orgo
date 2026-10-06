package websocket

import "github.com/ordaen/orgo/events"

// The actions of the record changes sent by ForwardEvents.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// ForwardEvents sends the record changes published to events.Creates, events.Updates and events.Deletes,
// like by the repositories created with repo.WithEvents, to the connections subscribed to them: it calls
// SendObjectID with ActionCreate, ActionUpdate or ActionDelete, the table and the record ID.
// It is not started by NewChan, an application forwards the changes on its own way by subscribing to the hubs
// and calling SendObjectID. The returned function stops forwarding.
//
//	stop := ws.ForwardEvents()
//	defer stop()
func (h *Chan) ForwardEvents() (stop func()) {
	forward := func(hub *events.Hub, action string) func() {
		return hub.Sub(events.All, func(e events.Event) {
			if id := e.ID(); id != "" {
				h.SendObjectID(action, e.Name, id)
			}
		})
	}
	stops := []func(){
		forward(events.Creates, ActionCreate),
		forward(events.Updates, ActionUpdate),
		forward(events.Deletes, ActionDelete),
	}
	return func() {
		for _, s := range stops {
			s()
		}
	}
}
