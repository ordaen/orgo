// Package events publishes record changes to subscribers through the Creates, Updates and Deletes channels.
package events

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/ordaen/orgo/model"
)

// Model is model.Model, the models published by PubModel.
type Model = model.Model

// Event describes a change of the record with ID in the table. The change is given by the channel it is published to.
type Event struct {
	TableName string
	ID        string
}

// The channels the repositories created with repo.WithEvents publish their changes to.
var (
	// Creates receives the events of the created records.
	Creates = NewChannel()
	// Updates receives the events of the updated records.
	Updates = NewChannel()
	// Deletes receives the events of the deleted records.
	Deletes = NewChannel()
)

// Channel delivers its published events to all its subscribers.
//
// Publishing never blocks: every subscriber has its own queue and goroutine, so a slow handler only delays itself.
// Each subscriber receives the events in the order they were published. When the queue of a subscriber is full,
// its oldest queued event is dropped and the number of dropped events is logged with slog.Default().
// A panic in a handler is recovered and logged, the subscriber continues with the next event.
type Channel struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

// NewChannel creates a channel.
func NewChannel() *Channel {
	return &Channel{subs: make(map[*subscriber]struct{})}
}

// Pub publishes the event of the record with id in the table. The id is converted to a string,
// so Pub("users", 1) and Pub("users", model.ID(1)) publish the same event.
func (c *Channel) Pub(tableName string, id any) {
	e := Event{TableName: tableName, ID: idString(id)}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for s := range c.subs {
		s.push(e)
	}
}

// PubModel publishes the event of the model, using its table name and ID.
func (c *Channel) PubModel(m Model) {
	c.Pub(m.TableName(), m.GetID())
}

// DefaultQueueLimit is the maximum number of queued events of a subscriber, unless it is set by WithQueueLimit.
const DefaultQueueLimit = 10000

// SubOption configures a subscriber created by Sub.
type SubOption func(*subscriber)

// WithQueueLimit sets the maximum number of events queued for the subscriber while its handler runs,
// DefaultQueueLimit without it. A limit <= 0 means no limit.
func WithQueueLimit(limit int) SubOption {
	return func(s *subscriber) {
		s.limit = limit
	}
}

// Sub calls handler for every event published after it returns, until unsubscribe is called.
// Events still queued when unsubscribe is called are dropped. unsubscribe can be called more than once.
func (c *Channel) Sub(handler func(Event), opts ...SubOption) (unsubscribe func()) {
	s := &subscriber{
		handler: handler,
		limit:   DefaultQueueLimit,
		notify:  make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	c.mu.Lock()
	c.subs[s] = struct{}{}
	c.mu.Unlock()
	go s.run()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, s)
			c.mu.Unlock()
			close(s.done)
		})
	}
}

// subscriber queues the events of one handler and calls it from its own goroutine.
type subscriber struct {
	handler func(Event)
	limit   int // maximum number of queued events, no limit when <= 0
	mu      sync.Mutex
	queue   []Event
	dropped int           // events dropped since the handler took the last batch
	notify  chan struct{} // signals new events in the queue
	done    chan struct{} // closed on unsubscribe
}

func (s *subscriber) push(e Event) {
	s.mu.Lock()
	if s.limit > 0 && len(s.queue) >= s.limit {
		// drop the oldest event, the queue is reallocated by append when its capacity is used up
		s.queue = s.queue[1:]
		s.dropped++
	}
	s.queue = append(s.queue, e)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default: // already signaled
	}
}

func (s *subscriber) run() {
	for {
		select {
		case <-s.done:
			return
		case <-s.notify:
		}
		for {
			s.mu.Lock()
			batch, dropped := s.queue, s.dropped
			s.queue, s.dropped = nil, 0
			s.mu.Unlock()
			if dropped > 0 {
				slog.Warn("events: subscriber queue is full, oldest events dropped",
					slog.Int("dropped", dropped), slog.Int("limit", s.limit))
			}
			if len(batch) == 0 {
				break
			}
			for _, e := range batch {
				select {
				case <-s.done:
					return
				default:
				}
				s.handle(e)
			}
		}
	}
}

// handle calls the handler, recovering and logging its panic.
func (s *subscriber) handle(e Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("events: handler panicked", slog.Any("panic", r),
				slog.String("table", e.TableName), slog.String("id", e.ID), slog.String("stack", string(debug.Stack())))
		}
	}()
	s.handler(e)
}

// idString converts the ID to the event ID.
func idString(id any) string {
	if mid, ok := id.(model.ModelID); ok {
		return mid.String()
	}
	return fmt.Sprint(id)
}
