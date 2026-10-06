// Package websocket sends application messages to the authorized browser connections of a Chan.
//
//	ws := websocket.NewChan(func(token string) *types.User {
//		if s := sessions.Sessions.FindByToken(token, "login"); s != nil {
//			return s.User()
//		}
//		return nil
//	})
//	http.HandleFunc("/ws", ws.Handler)
//	ws.SendToChannel("notifications", notification)
package websocket

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/types"
)

// Connection defaults, used for the zero Chan settings.
const (
	// DefaultMaxMessageSize is the maximum size of a message read from a connection, a larger one closes it.
	DefaultMaxMessageSize = 64 << 10
	// DefaultSendQueue is the number of messages queued for a connection, a slow connection is closed when it is full.
	DefaultSendQueue = 256
	// writeWait is the time a message write may take.
	writeWait = 10 * time.Second
	// pongWait is the time a connection may be silent, it is pinged every pingPeriod.
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10
)

// AuthorizeFunc returns the user of the token, or nil when it is not valid
type AuthorizeFunc func(token string) *types.User

// NewChan returns a Chan authorizing its connections with auth. It works right away, no goroutine is started for it.
func NewChan(auth AuthorizeFunc) *Chan {
	return &Chan{auth: auth, connections: make(map[*connection]struct{})}
}

// Chan holds the websocket connections and sends messages to the authorized ones.
// Its settings must be set before Handler is used.
type Chan struct {
	// DisableUsers stops sending the list of the connected users to the connections when it changes
	DisableUsers bool
	// MaxMessageSize is the maximum size of a message read from a connection, DefaultMaxMessageSize when it is 0
	MaxMessageSize int64
	// SendQueue is the number of messages queued for a connection, DefaultSendQueue when it is 0
	SendQueue int

	auth        AuthorizeFunc
	mu          sync.RWMutex
	connections map[*connection]struct{}
}

var upgrader = &websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: checkOrigin}

// checkOrigin allows the requests without Origin and the ones from the host of the request, on any port.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), hostname(r.Host))
}

// hostname returns the host without the port, also for the IPv6 addresses.
func hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return strings.Trim(hostport, "[]")
}

// Handler upgrades the request to a websocket connection and serves it until it is closed
func (h *Chan) Handler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logs.Error(err, "Websocket handshake error: ")
		return
	}
	c := newConnection(h, ws)
	h.mu.Lock()
	h.connections[c] = struct{}{}
	h.mu.Unlock()
	go c.writer()
	c.reader()
	c.close()
}

// remove removes the closed connection, the users are updated when it was authorized.
func (h *Chan) remove(c *connection) {
	h.mu.Lock()
	delete(h.connections, c)
	h.mu.Unlock()
	if c.authorized() {
		h.updateUsers()
	}
}

func (h *Chan) maxMessageSize() int64 {
	if h.MaxMessageSize > 0 {
		return h.MaxMessageSize
	}
	return DefaultMaxMessageSize
}

func (h *Chan) sendQueue() int {
	if h.SendQueue > 0 {
		return h.SendQueue
	}
	return DefaultSendQueue
}

// targets returns the authorized connections matching match.
func (h *Chan) targets(match func(c *connection) bool) []*connection {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var res []*connection
	for c := range h.connections {
		if c.authorized() && match(c) {
			res = append(res, c)
		}
	}
	return res
}

// broadcast queues the data to the authorized connections, except the sender, of the user uid or of all users
// when it is not valid. The connections are taken under the lock and sent to after it, a full queue closes
// its connection, which takes the lock.
func (h *Chan) broadcast(data []byte, uid model.ID, sender *connection) {
	for _, c := range h.targets(func(c *connection) bool {
		return c != sender && (!uid.Valid() || c.userID() == uid)
	}) {
		c.enqueue(data)
	}
}

var authSuccess = mustMarshal(socketPacket{Chan: "authorized"})

// usersPackage returns the users of the authorized connections.
func (h *Chan) usersPackage() []byte {
	seen := make(map[model.ID]bool)
	users := []types.User{}
	for _, c := range h.targets(func(*connection) bool { return true }) {
		if u := c.currentUser(); u != nil && !seen[u.ID] {
			seen[u.ID] = true
			users = append(users, types.User{ID: u.ID, Name: u.Name})
		}
	}
	return mustMarshal(socketPacket{Chan: "users", Data: packetObj{Name: "users", Data: users}})
}

// updateUsers sends the users to the authorized connections, unless DisableUsers is set.
func (h *Chan) updateUsers() {
	if !h.DisableUsers {
		h.broadcast(h.usersPackage(), 0, nil)
	}
}

// SendString sends s to all authorized connections
func (h *Chan) SendString(s string) {
	h.broadcast([]byte(s), 0, nil)
}

// SendToUserChannel sends obj in the channel to the authorized connections of the user
func (h *Chan) SendToUserChannel(uid model.ID, channel string, obj any) {
	h.send(socketPacket{Chan: channel, Data: obj}, uid)
}

// SendToChannel sends obj in the channel to all authorized connections
func (h *Chan) SendToChannel(channel string, obj any) {
	h.send(socketPacket{Chan: channel, Data: obj}, 0)
}

// SendObject sends the named obj in the channel to all authorized connections
func (h *Chan) SendObject(channel, name string, obj any) {
	h.send(socketPacket{Chan: channel, Data: packetObj{Name: name, Data: obj}}, 0)
}

// SendObjectToUser sends the named obj in the channel to the authorized connections of the user
func (h *Chan) SendObjectToUser(uid model.ID, channel, name string, obj any) {
	h.send(socketPacket{Chan: channel, Data: packetObj{Name: name, Data: obj}}, uid)
}

// SendObjectID sends the action on the model name with the id to the connections subscribed to it,
// by the subscribe command or to its collection by the subscribeCollections command
func (h *Chan) SendObjectID(action, name, id string) {
	data := mustMarshal(socketPacket{Chan: action, Data: packetObj{Name: name, Data: ider{ID: id}}})
	for _, c := range h.targets(func(c *connection) bool { return c.hasModel(name, id) }) {
		c.enqueue(data)
	}
}

// LogoutUser sends logout to the authorized connections of the user
func (h *Chan) LogoutUser(id model.ID) {
	h.send(socketPacket{Chan: "logout"}, id)
}

func (h *Chan) send(p socketPacket, uid model.ID) {
	data, err := json.Marshal(p)
	if err != nil {
		logs.Error(err, "[WEBSOCKET] marshal "+p.Chan+":")
		return
	}
	h.broadcast(data, uid, nil)
}

type packetObj struct {
	Name string `json:"name,omitempty"`
	Data any    `json:"data,omitempty"`
}

// PacketJSON is a named packet with its data encoded as a JSON string.
type PacketJSON struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

type socketPacket struct {
	Chan string `json:"chan"`
	Data any    `json:"data,omitempty"`
}

type ider struct {
	ID string `json:"id,omitempty"`
}

// mustMarshal marshals the packets of the package types, which can not fail.
func mustMarshal(p socketPacket) []byte {
	b, _ := json.Marshal(p)
	return b
}
