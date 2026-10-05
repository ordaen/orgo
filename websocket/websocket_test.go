package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuth authorizes the tokens "user-<id>".
func testAuth(token string) *types.User {
	var id int
	if _, err := fmt.Sscanf(token, "user-%d", &id); err != nil {
		return nil
	}
	return &types.User{ID: model.ID(id), Name: fmt.Sprintf("User %d", id)}
}

func newTestChan(t *testing.T) (*Chan, string) {
	h := NewChan(testAuth)
	srv := httptest.NewServer(http.HandlerFunc(h.Handler))
	t.Cleanup(srv.Close)
	return h, "ws" + strings.TrimPrefix(srv.URL, "http")
}

// client is a test connection, its messages are read in the background into msgs,
// a read deadline would break the connection for the next reads.
type client struct {
	t    *testing.T
	conn *websocket.Conn
	msgs chan string
}

func dial(t *testing.T, url string) *client {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	c := &client{t: t, conn: conn, msgs: make(chan string, 1000)}
	go func() {
		defer close(c.msgs)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			c.msgs <- string(msg)
		}
	}()
	return c
}

func (c *client) cmd(cmd string, fields map[string]any) {
	c.t.Helper()
	msg := map[string]any{"cmd": cmd}
	for k, v := range fields {
		msg[k] = v
	}
	require.NoError(c.t, c.conn.WriteJSON(msg))
}

// authorize authorizes the client as the user id and reads the authorized and users messages.
func (c *client) authorize(id int) {
	c.t.Helper()
	c.cmd("authorize", map[string]any{"data": fmt.Sprintf("user-%d", id)})
	assert.Equal(c.t, `{"chan":"authorized"}`, c.read())
}

func (c *client) read() string {
	c.t.Helper()
	select {
	case msg, ok := <-c.msgs:
		require.True(c.t, ok, "connection closed")
		return msg
	case <-time.After(time.Second):
		c.t.Fatal("message not received")
		return ""
	}
}

// readUntil reads the messages until one contains s.
func (c *client) readUntil(s string) string {
	c.t.Helper()
	for {
		if msg := c.read(); strings.Contains(msg, s) {
			return msg
		}
	}
}

func (c *client) assertNoMessage() {
	c.t.Helper()
	select {
	case msg, ok := <-c.msgs:
		if ok {
			c.t.Errorf("unexpected message %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

// connected waits until the chan has n connections.
func connected(t *testing.T, h *Chan, n int) {
	t.Helper()
	assert.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.connections) == n
	}, time.Second, 5*time.Millisecond)
}

func TestAuthorize(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	c := dial(t, url)
	c.cmd("authorize", map[string]any{"data": "bad token"})
	c.assertNoMessage()

	c.authorize(1)
	h.SendToChannel("news", "hello")
	assert.Equal(t, `{"chan":"news","data":"hello"}`, c.read())
}

func TestSendOnlyToAuthorized(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	authorized, anonymous := dial(t, url), dial(t, url)
	authorized.authorize(1)
	connected(t, h, 2)

	h.SendString("plain")
	h.SendObject("objects", "order", map[string]int{"id": 3})
	assert.Equal(t, "plain", authorized.read())
	assert.Equal(t, `{"chan":"objects","data":{"name":"order","data":{"id":3}}}`, authorized.read())
	anonymous.assertNoMessage()
}

func TestSendToUser(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	first, second, other := dial(t, url), dial(t, url), dial(t, url)
	first.authorize(1)
	second.authorize(1)
	other.authorize(2)

	h.SendToUserChannel(model.ID(1), "private", "secret")
	h.SendObjectToUser(model.ID(1), "objects", "order", 5)
	h.LogoutUser(model.ID(1))
	for _, c := range []*client{first, second} {
		assert.Equal(t, `{"chan":"private","data":"secret"}`, c.read())
		assert.Equal(t, `{"chan":"objects","data":{"name":"order","data":5}}`, c.read())
		assert.Equal(t, `{"chan":"logout"}`, c.read())
	}
	other.assertNoMessage()
}

// TestPing checks the pong is sent to the pinging connection only.
func TestPing(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	pinger, other := dial(t, url), dial(t, url)
	other.authorize(2)
	pinger.cmd("ping", nil)
	assert.Equal(t, "pong", pinger.read())
	other.assertNoMessage()
}

// TestRelay checks the unknown commands of the authorized connections are relayed to the other authorized ones.
func TestRelay(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	sender, receiver, anonymous := dial(t, url), dial(t, url), dial(t, url)
	sender.authorize(1)
	receiver.authorize(2)

	anonymous.cmd("chat", map[string]any{"data": "spam"})
	receiver.assertNoMessage()

	sender.cmd("chat", map[string]any{"data": "hi"})
	assert.JSONEq(t, `{"cmd":"chat","data":"hi"}`, receiver.read())
	sender.assertNoMessage()
	anonymous.assertNoMessage()
}

func TestSendObjectID(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	byID, byCollection, unsubscribed := dial(t, url), dial(t, url), dial(t, url)
	byID.authorize(1)
	byCollection.authorize(2)
	unsubscribed.authorize(3)
	byID.cmd("subscribe", map[string]any{"models": map[string][]string{"orders": {"5", "6"}}})
	byCollection.cmd("subscribeCollections", map[string]any{"collections": []string{"orders"}})
	// the subscriptions are handled in order, a ping after them is answered after them
	byID.cmd("ping", nil)
	assert.Equal(t, "pong", byID.read())
	byCollection.cmd("ping", nil)
	assert.Equal(t, "pong", byCollection.read())

	h.SendObjectID("update", "orders", "6")
	h.SendObjectID("update", "orders", "7")
	assert.Equal(t, `{"chan":"update","data":{"name":"orders","data":{"id":"6"}}}`, byID.read())
	assert.Equal(t, `{"chan":"update","data":{"name":"orders","data":{"id":"6"}}}`, byCollection.read())
	assert.Equal(t, `{"chan":"update","data":{"name":"orders","data":{"id":"7"}}}`, byCollection.read())
	byID.assertNoMessage()
	unsubscribed.assertNoMessage()
}

func TestSubscribeRequiresAuthorization(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	c := dial(t, url)
	c.cmd("subscribeCollections", map[string]any{"collections": []string{"orders"}})
	c.cmd("authorize", map[string]any{"data": "user-1"})
	assert.Equal(t, `{"chan":"authorized"}`, c.read())
	h.SendObjectID("update", "orders", "1")
	c.assertNoMessage()
}

// TestUsers checks the users list is sent on authorize and on disconnect.
func TestUsers(t *testing.T) {
	_, url := newTestChan(t)
	first := dial(t, url)
	first.authorize(1)
	assert.JSONEq(t, `{"chan":"users","data":{"name":"users","data":[{"id":"1","name":"User 1"}]}}`, first.read())

	second := dial(t, url)
	second.authorize(2)
	users := first.readUntil(`"users"`)
	assert.Contains(t, users, `"name":"User 1"`)
	assert.Contains(t, users, `"name":"User 2"`)

	second.conn.Close()
	assert.JSONEq(t, `{"chan":"users","data":{"name":"users","data":[{"id":"1","name":"User 1"}]}}`, first.readUntil(`"users"`),
		"the disconnected user is removed")

	first.cmd("list-users", nil)
	assert.JSONEq(t, `{"chan":"users","data":{"name":"users","data":[{"id":"1","name":"User 1"}]}}`, first.read())
}

func TestDisconnectRemovesConnection(t *testing.T) {
	h, url := newTestChan(t)
	c := dial(t, url)
	connected(t, h, 1)
	c.conn.Close()
	connected(t, h, 0)
}

func TestMaxMessageSize(t *testing.T) {
	h, url := newTestChan(t)
	h.MaxMessageSize = 100
	c := dial(t, url)
	connected(t, h, 1)
	require.NoError(t, c.conn.WriteMessage(websocket.TextMessage, []byte(`{"cmd":"x","data":"`+strings.Repeat("a", 200)+`"}`)))
	connected(t, h, 0)
}

// TestSlowConnectionClosed checks a connection that does not read is closed when its queue is full,
// without blocking the sender or the other connections.
func TestSlowConnectionClosed(t *testing.T) {
	h, url := newTestChan(t)
	h.DisableUsers = true
	h.SendQueue = 4
	slowConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { slowConn.Close() })
	require.NoError(t, slowConn.WriteJSON(map[string]any{"cmd": "authorize", "data": "user-1"}))
	fast := dial(t, url)
	fast.authorize(2)
	connected(t, h, 2)

	big := strings.Repeat("x", 64<<10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			h.SendToChannel("big", big)
		}
	}()
	go func() {
		for range fast.msgs {
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sending blocked on the slow connection")
	}
	connected(t, h, 1)
}

func TestConcurrent(t *testing.T) {
	h, url := newTestChan(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			c := dial(t, url)
			c.cmd("authorize", map[string]any{"data": fmt.Sprintf("user-%d", i%3+1)})
			c.cmd("subscribeCollections", map[string]any{"collections": []string{"orders"}})
			c.cmd("set-path", map[string]any{"data": "/orders"})
			c.cmd("list-users", nil)
			c.cmd("chat", map[string]any{"data": "hi"})
			c.conn.Close()
		})
		wg.Go(func() {
			h.SendToChannel("news", i)
			h.SendObjectID("update", "orders", "1")
			h.SendToUserChannel(model.ID(1), "private", i)
		})
	}
	wg.Wait()
	connected(t, h, 0)
}

func TestCheckOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin, host string
		want         bool
	}{
		{"", "example.com", true},
		{"https://example.com", "example.com", true},
		{"https://example.com:8443", "example.com:8080", true},
		{"https://EXAMPLE.com", "example.com", true},
		{"https://evil.com", "example.com", false},
		{"https://[::1]:8443", "[::1]:8080", true},
		{"https://[::1]", "[::2]", false},
		{"://bad", "example.com", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = tc.host
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		assert.Equal(t, tc.want, checkOrigin(r), "%s from %s", tc.host, tc.origin)
	}
}

func TestUsersPackageEmpty(t *testing.T) {
	h := NewChan(nil)
	var p struct {
		Data struct{ Data []types.User }
	}
	require.NoError(t, json.Unmarshal(h.usersPackage(), &p))
	assert.Empty(t, p.Data.Data)
}
