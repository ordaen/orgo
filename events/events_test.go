package events

import (
	"bytes"
	"encoding/json"
	"log/slog"
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

// collect subscribes to c and returns a channel receiving the handled events.
func collect(t *testing.T, c *Channel) <-chan Event {
	t.Helper()
	ch := make(chan Event, 100)
	t.Cleanup(c.Sub(func(e Event) { ch <- e }))
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

func TestChannels(t *testing.T) {
	creates, updates, deletes := collect(t, Creates), collect(t, Updates), collect(t, Deletes)

	Creates.Pub("users", 1)
	Updates.Pub("users", model.ID(2))
	Deletes.Pub("orders", "3")

	assert.Equal(t, Event{TableName: "users", ID: "1"}, receive(t, creates))
	assert.Equal(t, Event{TableName: "users", ID: "2"}, receive(t, updates))
	assert.Equal(t, Event{TableName: "orders", ID: "3"}, receive(t, deletes))
	// channels are separate
	assertNoEvent(t, creates)
	assertNoEvent(t, updates)
	assertNoEvent(t, deletes)
}

func TestPubModel(t *testing.T) {
	c := NewChannel()
	ch := collect(t, c)

	c.PubModel(&user{Base: model.Base[model.ID]{ID: 7}})
	assert.Equal(t, Event{TableName: "users", ID: "7"}, receive(t, ch))
}

func TestPubWithoutSubscribers(t *testing.T) {
	c := NewChannel()
	done := make(chan struct{})
	go func() {
		c.Pub("users", 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Pub must not block without subscribers")
	}
}

func TestAllSubscribersReceiveEvents(t *testing.T) {
	c := NewChannel()
	first, second := collect(t, c), collect(t, c)

	for i := range 10 {
		c.Pub("users", i)
	}
	// every subscriber gets every event, in the published order
	for i := range 10 {
		assert.Equal(t, idString(i), receive(t, first).ID)
		assert.Equal(t, idString(i), receive(t, second).ID)
	}
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	c := NewChannel()
	release := make(chan struct{})
	slow := make(chan Event, 100)
	defer c.Sub(func(e Event) {
		<-release
		slow <- e
	})()
	fast := collect(t, c)

	// Pub and the other subscriber are not blocked by the slow handler
	for i := range 50 {
		c.Pub("users", i)
	}
	for i := range 50 {
		assert.Equal(t, idString(i), receive(t, fast).ID)
	}

	close(release)
	for i := range 50 {
		assert.Equal(t, idString(i), receive(t, slow).ID, "queued events are delivered in order")
	}
}

func TestUnsubscribe(t *testing.T) {
	c := NewChannel()
	ch := make(chan Event, 10)
	unsubscribe := c.Sub(func(e Event) { ch <- e })

	c.Pub("users", 1)
	receive(t, ch)

	unsubscribe()
	unsubscribe() // can be called more than once
	c.Pub("users", 2)
	assertNoEvent(t, ch)
	assert.Empty(t, c.subs)
}

func TestConcurrentPubSub(t *testing.T) {
	c := NewChannel()
	var received sync.WaitGroup
	const publishers, events = 10, 20
	received.Add(publishers * events)
	defer c.Sub(func(Event) { received.Done() })()

	var wg sync.WaitGroup
	for range publishers {
		wg.Go(func() {
			for i := range events {
				c.Pub("users", i)
			}
		})
		// subscribing and unsubscribing while publishing is safe
		wg.Go(func() { c.Sub(func(Event) {})() })
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

func TestIDString(t *testing.T) {
	require.Equal(t, "1", idString(1))
	require.Equal(t, "1", idString(model.ID(1)))
	require.Equal(t, "abc", idString("abc"))
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
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestQueueLimitDropsOldestEvents(t *testing.T) {
	logs := captureLogs(t)
	c := NewChannel()
	started := make(chan struct{})
	release := make(chan struct{})
	received := make(chan Event, 100)
	defer c.Sub(func(e Event) {
		if e.ID == "0" {
			close(started)
			<-release
		}
		received <- e
	}, WithQueueLimit(3))()

	// the handler is busy with the first event, the next ones are queued up to the limit
	c.Pub("users", 0)
	<-started
	for i := 1; i <= 9; i++ {
		c.Pub("users", i)
	}
	close(release)

	var ids []string
	for range 4 {
		ids = append(ids, receive(t, received).ID)
	}
	assert.Equal(t, []string{"0", "7", "8", "9"}, ids, "the oldest queued events are dropped")
	assert.Eventually(t, func() bool {
		return strings.Contains(logs.String(), `"msg":"events: subscriber queue is full, oldest events dropped","dropped":6,"limit":3`)
	}, time.Second, 10*time.Millisecond)

	// the dropped events are counted again after they were logged
	for i := 10; i <= 11; i++ {
		c.Pub("users", i)
	}
	assert.Equal(t, "10", receive(t, received).ID)
	assert.Equal(t, "11", receive(t, received).ID)
	assert.Equal(t, 1, strings.Count(logs.String(), "oldest events dropped"))
}

func TestQueueWithoutLimit(t *testing.T) {
	c := NewChannel()
	release := make(chan struct{})
	received := make(chan Event, 1000)
	defer c.Sub(func(e Event) {
		<-release
		received <- e
	}, WithQueueLimit(0))()

	for i := range 500 {
		c.Pub("users", i)
	}
	close(release)
	for i := range 500 {
		assert.Equal(t, idString(i), receive(t, received).ID)
	}
}

func TestHandlerPanicIsRecovered(t *testing.T) {
	logs := captureLogs(t)
	c := NewChannel()
	received := make(chan Event, 10)
	defer c.Sub(func(e Event) {
		if e.ID == "1" {
			panic("handler failed")
		}
		received <- e
	})()

	for i := range 3 {
		c.Pub("users", i)
	}
	// the subscriber continues with the next events
	assert.Equal(t, "0", receive(t, received).ID)
	assert.Equal(t, "2", receive(t, received).ID)

	var record struct {
		Level, Msg, Panic, Table, ID, Stack string
	}
	require.NoError(t, json.Unmarshal([]byte(logs.String()), &record))
	assert.Equal(t, "ERROR", record.Level)
	assert.Equal(t, "events: handler panicked", record.Msg)
	assert.Equal(t, "handler failed", record.Panic)
	assert.Equal(t, "users", record.Table)
	assert.Equal(t, "1", record.ID)
	assert.Contains(t, record.Stack, "TestHandlerPanicIsRecovered")
}
