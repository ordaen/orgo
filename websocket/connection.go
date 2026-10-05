package websocket

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/types"
)

var pong = []byte("pong")

type messageIn struct {
	Command     string              `json:"cmd"`
	Data        any                 `json:"data"`
	Models      map[string][]string `json:"models"`
	Collections []string            `json:"collections"`
}

func newConnection(hub *Chan, conn *websocket.Conn) *connection {
	return &connection{
		hub:              hub,
		conn:             conn,
		send:             make(chan []byte, hub.sendQueue()),
		done:             make(chan struct{}),
		subscribedModels: make(subscribedModels),
	}
}

// connection is a websocket connection. Its reader handles the commands in order, its writer is the only writer
// of the messages. send is never closed: close closes done, so queueing to a closed connection does nothing.
type connection struct {
	hub       *Chan
	conn      *websocket.Conn
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once

	mu               sync.RWMutex
	user             *types.User
	page             string
	subscribedModels subscribedModels
	collections      []string
}

// enqueue queues the message for the writer. A connection with a full queue is too slow, it is closed.
func (c *connection) enqueue(data []byte) {
	select {
	case <-c.done:
	case c.send <- data:
	default:
		c.close()
	}
}

// close closes the connection and removes it from the hub, it can be called more than once.
func (c *connection) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.conn.Close()
		c.hub.remove(c)
	})
}

func (c *connection) reader() {
	c.conn.SetReadLimit(c.hub.maxMessageSize())
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.handle(message)
	}
}

// handle runs the command of the message.
func (c *connection) handle(message []byte) {
	var msg messageIn
	if err := json.Unmarshal(message, &msg); err != nil {
		logs.Error(err, "[WEBSOCKET]: ")
		return
	}
	switch msg.Command {
	case "ping":
		c.enqueue(pong)
	case "authorize":
		c.authorize(msg)
	case "set-path":
		c.setPath(msg)
	case "subscribeCollections":
		c.subscribeCollections(msg)
	case "subscribe":
		c.subscribeModels(msg)
	case "list-users":
		c.enqueue(c.hub.usersPackage())
	default:
		// the other messages of the authorized connections are relayed to the other authorized connections
		if c.authorized() {
			c.hub.broadcast(message, 0, c)
		}
	}
}

func (c *connection) writer() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.close()
	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}

func (c *connection) authorize(msg messageIn) {
	token, _ := msg.Data.(string)
	var user *types.User
	if c.hub.auth != nil {
		user = c.hub.auth(token)
	}
	if user == nil || !user.ID.Valid() {
		user = nil
	}
	c.mu.Lock()
	was := c.user != nil
	c.user = user
	c.mu.Unlock()
	if user != nil {
		c.enqueue(authSuccess)
	}
	if user != nil || was {
		c.hub.updateUsers()
	}
}

func (c *connection) authorized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user != nil
}

func (c *connection) currentUser() *types.User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

func (c *connection) userID() model.ID {
	if u := c.currentUser(); u != nil {
		return u.ID
	}
	return 0
}

func (c *connection) hasModel(name, id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Contains(c.collections, name) || c.subscribedModels.includes(name, id)
}

func (c *connection) subscribeCollections(msg messageIn) {
	if !c.authorized() {
		return
	}
	c.mu.Lock()
	c.collections = msg.Collections
	c.mu.Unlock()
}

func (c *connection) subscribeModels(msg messageIn) {
	if !c.authorized() {
		return
	}
	c.mu.Lock()
	for k, v := range msg.Models {
		c.subscribedModels.set(k, v...)
	}
	c.mu.Unlock()
}

func (c *connection) setPath(msg messageIn) {
	if !c.authorized() {
		return
	}
	if s, ok := msg.Data.(string); ok {
		c.mu.Lock()
		c.page = s
		c.mu.Unlock()
	}
}
