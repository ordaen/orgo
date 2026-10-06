package websocket

import (
	"testing"

	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
)

func TestForwardEvents(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	c := dial(t, url)
	c.authorize(1)
	c.cmd("subscribeCollections", map[string]any{"collections": []string{"orders"}})
	c.cmd("ping", nil)
	assert.Equal(t, "pong", c.read())

	// not forwarded before ForwardEvents
	events.Creates.PubID("orders", model.ID(1))
	c.assertNoMessage()

	stop := h.ForwardEvents()
	events.Creates.PubID("orders", model.ID(1))
	assert.Equal(t, `{"chan":"create","data":{"name":"orders","data":{"id":"1"}}}`, c.read())
	events.Updates.PubID("orders", model.UUID("2a"))
	assert.Equal(t, `{"chan":"update","data":{"name":"orders","data":{"id":"2a"}}}`, c.read())
	events.Deletes.PubID("orders", model.ID(3))
	assert.Equal(t, `{"chan":"delete","data":{"name":"orders","data":{"id":"3"}}}`, c.read())

	// the other tables and the events without ID are not sent
	events.Creates.PubID("users", model.ID(4))
	events.Creates.PubEvent("orders")
	c.assertNoMessage()

	stop()
	stop()
	events.Creates.PubID("orders", model.ID(5))
	c.assertNoMessage()
}
