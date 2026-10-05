package events

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type user struct {
	model.Base[model.ID]
	Name string
}

func (u *user) TableName() string {
	return "users"
}

type session struct {
	model.Base[model.UUID]
}

func (s *session) TableName() string {
	return "sessions"
}

// collect subscribes to the event name of h and returns a channel receiving the handled events.
func collect(t *testing.T, h *Hub, name string) <-chan Event {
	t.Helper()
	ch := make(chan Event, 100)
	t.Cleanup(h.Sub(name, func(e Event) { ch <- e }))
	return ch
}

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("event not received")
		return Event{}
	}
}

func assertNoEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("unexpected event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

// pubN publishes the events of the name with the data from to to-1.
func pubN(h *Hub, name string, from, to int) {
	for i := from; i < to; i++ {
		h.PubData(name, i)
	}
}

func TestHubs(t *testing.T) {
	system, creates, updates, deletes := collect(t, System, All), collect(t, Creates, All), collect(t, Updates, All), collect(t, Deletes, All)

	System.PubEvent("started")
	Creates.PubID("users", model.ID(1))
	Updates.PubID("users", model.ID(2))
	Deletes.PubID("orders", model.ID(3))
	assert.Equal(t, "started", receive(t, system).Name)
	assert.Equal(t, Event{Name: "users", Data: model.ID(1)}, receive(t, creates))
	assert.Equal(t, Event{Name: "users", Data: model.ID(2)}, receive(t, updates))
	assert.Equal(t, Event{Name: "orders", Data: model.ID(3)}, receive(t, deletes))
	// hubs are separate
	for _, ch := range []<-chan Event{system, creates, updates, deletes} {
		assertNoEvent(t, ch)
	}
}

func TestPub(t *testing.T) {
	h := NewHub("TEST")
	ch := collect(t, h, "users")

	u := &user{ID: 7, Name: "john"}
	h.Pub(u)
	u.Name = "changed after publishing"
	e := receive(t, ch)
	assert.Equal(t, "users", e.Name)
	assert.Equal(t, "7", e.ID())
	doc, ok := e.Doc().(*user)
	require.True(t, ok)
	assert.NotSame(t, u, doc)
	assert.Equal(t, "john", doc.Name, "the subscribers get a copy")
}

func TestPubVariants(t *testing.T) {
	h := NewHub("TEST")
	ch := collect(t, h, All)

	h.PubEvent("ping")
	e := receive(t, ch)
	assert.Equal(t, Event{Name: "ping"}, e)
	assert.Nil(t, e.Doc())
	assert.Empty(t, e.ID())

	h.PubData("report", map[string]int{"rows": 3})
	e = receive(t, ch)
	assert.Equal(t, map[string]int{"rows": 3}, e.Data)
	assert.Nil(t, e.Doc())
	assert.Empty(t, e.ID(), "data is not an ID")

	h.PubID("users", model.ID(5))
	assert.Equal(t, "5", receive(t, ch).ID())

	// the models and the IDs of any type
	h.PubID("sessions", model.UUID("0b6f"))
	assert.Equal(t, "0b6f", receive(t, ch).ID())
	h.Pub(&session{ID: "1c7a"})
	assert.Equal(t, "1c7a", receive(t, ch).ID())
}

func TestSubByName(t *testing.T) {
	h := NewHub("TEST")
	users, orders, all := collect(t, h, "users"), collect(t, h, "orders"), collect(t, h, All)

	h.PubEvent("users")
	h.PubEvent("orders")
	h.PubEvent("other")
	assert.Equal(t, "users", receive(t, users).Name)
	assert.Equal(t, "orders", receive(t, orders).Name)
	for _, name := range []string{"users", "orders", "other"} {
		assert.Equal(t, name, receive(t, all).Name, "All receives every event in order")
	}
	assertNoEvent(t, users)
	assertNoEvent(t, orders)
}

func TestSubFunc(t *testing.T) {
	h := NewHub("TEST")
	ch := make(chan Event, 100)
	unsubscribe := h.SubFunc(func(e Event) { ch <- e }, "users", "orders", "users", All)

	h.PubData("users", 1)
	h.PubData("orders", 2)
	h.PubData("other", 3)
	// one queue in the published order, an event is received once also for repeated names and All
	for i := 1; i <= 3; i++ {
		assert.Equal(t, i, receive(t, ch).Data)
	}
	assertNoEvent(t, ch)

	unsubscribe()
	h.PubEvent("users")
	assertNoEvent(t, ch)
	assert.Empty(t, h.subs, "unsubscribed from all names")
}

func TestPubWithoutSubscribers(t *testing.T) {
	h := NewHub("TEST")
	done := make(chan struct{})
	go func() {
		pubN(h, "users", 0, 100)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Pub must not block without subscribers")
	}
}

func TestAllSubscribersReceiveEvents(t *testing.T) {
	h := NewHub("TEST")
	first, second := collect(t, h, "users"), collect(t, h, "users")

	pubN(h, "users", 0, 10)
	// every subscriber gets every event, in the published order
	for i := range 10 {
		assert.Equal(t, i, receive(t, first).Data)
		assert.Equal(t, i, receive(t, second).Data)
	}
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub("TEST")
	release := make(chan struct{})
	slow := make(chan Event, 100)
	defer h.Sub("users", func(e Event) {
		<-release
		slow <- e
	})()
	fast := collect(t, h, "users")

	// publishing and the other subscriber are not blocked by the slow handler
	pubN(h, "users", 0, 50)
	for i := range 50 {
		assert.Equal(t, i, receive(t, fast).Data)
	}

	close(release)
	for i := range 50 {
		assert.Equal(t, i, receive(t, slow).Data, "queued events are delivered in order")
	}
}

func TestUnsubscribe(t *testing.T) {
	h := NewHub("TEST")
	ch := make(chan Event, 10)
	unsubscribe := h.Sub("users", func(e Event) { ch <- e })
	kept := collect(t, h, "users")

	h.PubEvent("users")
	receive(t, ch)
	receive(t, kept)

	unsubscribe()
	unsubscribe() // can be called more than once
	h.PubEvent("users")
	assertNoEvent(t, ch)
	receive(t, kept)
	assert.Len(t, h.subs["users"], 1, "only the unsubscribed handler is removed")
}

// TestUnsubscribeSameFunc checks the subscriptions of the same function are removed separately.
func TestUnsubscribeSameFunc(t *testing.T) {
	h := NewHub("TEST")
	var mu sync.Mutex
	count := 0
	handler := func(Event) {
		mu.Lock()
		count++
		mu.Unlock()
	}
	unsubscribe := h.Sub("users", handler)
	defer h.Sub("users", handler)()
	unsubscribe()

	h.PubEvent("users")
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return count == 1
	}, time.Second, 5*time.Millisecond)
}

func TestConcurrentPubSub(t *testing.T) {
	h := NewHub("TEST")
	var received sync.WaitGroup
	const publishers, events = 10, 20
	received.Add(publishers * events)
	defer h.Sub(All, func(Event) { received.Done() })()

	var wg sync.WaitGroup
	for p := range publishers {
		name := strconv.Itoa(p)
		wg.Go(func() { pubN(h, name, 0, events) })
		// subscribing and unsubscribing while publishing is safe
		wg.Go(func() { h.Sub(name, func(Event) {})() })
		wg.Go(func() { h.SubFunc(func(Event) {}, name, All)() })
	}
	wg.Wait()

	done := make(chan struct{})
	go func() {
		received.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not all events received")
	}
}

func TestCopyModel(t *testing.T) {
	u := &user{Name: "john"}
	c := copyModel(u)
	assert.NotSame(t, u, c)
	assert.Equal(t, u, c)
	var nilUser *user
	assert.Same(t, nilUser, copyModel(nilUser).(*user), "a nil pointer is not copied")
}

// syncBuffer is a bytes.Buffer safe for the concurrent writes of the subscriber goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs sets slog.Default() to a JSON logger writing to the returned buffer until the test ends.
func captureLogs(t *testing.T, level slog.Level) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestDebugLog(t *testing.T) {
	logs := captureLogs(t, slog.LevelDebug)
	h := NewHub("TEST")
	h.Sub("users", func(Event) {})()
	h.PubID("users", model.ID(3))
	out := logs.String()
	assert.Contains(t, out, `"msg":"events: subscribe","hub":"TEST","names":["users"]`)
	assert.Contains(t, out, `"msg":"events: unsubscribe","hub":"TEST"`)
	assert.Contains(t, out, `"msg":"events: publish","hub":"TEST","event":{"name":"users","id":"3","type":"model.ID"}`)
}

func TestQueueLimitDropsOldestEvents(t *testing.T) {
	logs := captureLogs(t, slog.LevelInfo)
	h := NewHub("TEST")
	started := make(chan struct{})
	release := make(chan struct{})
	received := make(chan Event, 100)
	defer h.Sub("users", func(e Event) {
		if e.Data == 0 {
			close(started)
			<-release
		}
		received <- e
	}, WithQueueLimit(3))()

	// the handler is busy with the first event, the next ones are queued up to the limit
	h.PubData("users", 0)
	<-started
	pubN(h, "users", 1, 10)
	close(release)

	var got []any
	for range 4 {
		got = append(got, receive(t, received).Data)
	}
	assert.Equal(t, []any{0, 7, 8, 9}, got, "the oldest queued events are dropped")
	assert.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `"msg":"events: subscriber queue is full, oldest events dropped","hub":"TEST","dropped":6,"limit":3`)
	}, time.Second, 10*time.Millisecond)

	// the dropped events are counted again after they were logged
	pubN(h, "users", 10, 12)
	assert.Equal(t, 10, receive(t, received).Data)
	assert.Equal(t, 11, receive(t, received).Data)
	assert.Equal(t, 1, strings.Count(logs.String(), "oldest events dropped"))
}

func TestQueueWithoutLimit(t *testing.T) {
	h := NewHub("TEST")
	release := make(chan struct{})
	received := make(chan Event, 2000)
	defer h.Sub("users", func(e Event) {
		<-release
		received <- e
	}, WithQueueLimit(0))()

	pubN(h, "users", 0, 1500)
	close(release)
	for i := range 1500 {
		assert.Equal(t, i, receive(t, received).Data)
	}
}

func TestHandlerPanicIsRecovered(t *testing.T) {
	logs := captureLogs(t, slog.LevelInfo)
	h := NewHub("TEST")
	received := make(chan Event, 10)
	defer h.Sub("users", func(e Event) {
		if e.ID() == "1" {
			panic("handler failed")
		}
		received <- e
	})()

	for i := range 3 {
		h.PubID("users", model.ID(i))
	}
	// the subscriber continues with the next events
	assert.Equal(t, "0", receive(t, received).ID())
	assert.Equal(t, "2", receive(t, received).ID())

	var record struct {
		Level, Msg, Hub, Panic, Stack string
		Event                         struct{ Name, ID string }
	}
	require.NoError(t, json.Unmarshal([]byte(logs.String()), &record))
	assert.Equal(t, "ERROR", record.Level)
	assert.Equal(t, "events: handler panicked", record.Msg)
	assert.Equal(t, "TEST", record.Hub)
	assert.Equal(t, "handler failed", record.Panic)
	assert.Equal(t, "users", record.Event.Name)
	assert.Equal(t, "1", record.Event.ID)
	assert.Contains(t, record.Stack, "TestHandlerPanicIsRecovered")
}

func BenchmarkPub(b *testing.B) {
	h := NewHub("BENCH")
	for i := range 10 {
		defer h.Sub(strconv.Itoa(i), func(Event) {})()
	}
	defer h.Sub(All, func(Event) {})()
	u := &user{Name: "john"}
	for b.Loop() {
		h.Pub(u)
	}
}
