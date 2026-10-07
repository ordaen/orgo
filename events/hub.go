package events

import (
	"context"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
)

// Hub delivers its published events to the subscribers of their names and of All.
//
// Publishing never blocks: every subscriber has its own queue and goroutine, so a slow handler only delays itself.
// Each subscriber receives the events in the order they were published. When the queue of a subscriber is full,
// its oldest queued event is dropped and the number of dropped events is logged with slog.Default().
// A panic in a handler is recovered and logged, the subscriber continues with the next event.
type Hub struct {
	name string
	mu   sync.RWMutex
	subs map[string][]*subscriber // by event name
}

// NewHub creates a hub, the name is used in the log messages.
func NewHub(name string) *Hub {
	return &Hub{name: name, subs: make(map[string][]*subscriber)}
}

// Pub publishes the model as an event named by its type, returned by pg.ModelType, like "User" for models.User.
// The subscribers get a shallow copy of the model, so the publisher can change its fields after publishing.
func (h *Hub) Pub(m Model) {
	h.publish(Event{Name: pg.ModelType(m), Data: copyModel(m)})
}

// PubDoc publishes the model as the named event, the subscribers get a shallow copy like for Pub.
func (h *Hub) PubDoc(name string, m Model) {
	h.publish(Event{Name: name, Data: copyModel(m)})
}

// PubEvent publishes an event without data.
func (h *Hub) PubEvent(name string) {
	h.publish(Event{Name: name})
}

// PubData publishes an event with the data, it is shared by the subscribers.
func (h *Hub) PubData(name string, data any) {
	h.publish(Event{Name: name, Data: data})
}

// PubID publishes an event named by the model type, like Pub, with the record ID, returned by Event.ID.
func (h *Hub) PubID(modelType string, id model.ModelID) {
	h.publish(Event{Name: modelType, Data: id})
}

func (h *Hub) publish(e Event) {
	// the attributes are built only when they are logged, publishing is frequent
	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		slog.Debug("events: publish", slog.String("hub", h.name), slog.Any("event", e))
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	named, all := h.subs[e.Name], h.subs[All]
	for _, s := range named {
		s.push(e)
	}
	for _, s := range all {
		// a subscriber of the name and of All receives the event once
		if !slices.Contains(named, s) {
			s.push(e)
		}
	}
}

// Sub calls handler for every event of the name, or of all names for All, published after it returns,
// until unsubscribe is called. Events still queued when unsubscribe is called are dropped.
// unsubscribe can be called more than once.
func (h *Hub) Sub(name string, handler EventFunc, opts ...SubOption) (unsubscribe func()) {
	return h.subscribe(handler, []string{name}, opts)
}

// SubFunc is like Sub for the events of all names. The handler receives them in one queue, in the order
// they were published, and an event is received once also when its name is given twice or with All.
func (h *Hub) SubFunc(handler EventFunc, names ...string) (unsubscribe func()) {
	return h.subscribe(handler, names, nil)
}

func (h *Hub) subscribe(handler EventFunc, names []string, opts []SubOption) func() {
	s := &subscriber{
		hub:     h.name,
		handler: handler,
		limit:   DefaultQueueLimit,
		notify:  make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	names = slices.Compact(slices.Sorted(slices.Values(names)))
	h.mu.Lock()
	for _, name := range names {
		h.subs[name] = append(h.subs[name], s)
	}
	h.mu.Unlock()
	slog.Debug("events: subscribe", slog.String("hub", h.name), slog.Any("names", names))
	go s.run()

	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			for _, name := range names {
				subs := slices.DeleteFunc(h.subs[name], func(v *subscriber) bool { return v == s })
				if len(subs) == 0 {
					delete(h.subs, name)
				} else {
					h.subs[name] = subs
				}
			}
			h.mu.Unlock()
			close(s.done)
			slog.Debug("events: unsubscribe", slog.String("hub", h.name), slog.Any("names", names))
		})
	}
}

// DefaultQueueLimit is the maximum number of queued events of a subscriber, unless it is set by WithQueueLimit.
const DefaultQueueLimit = 1000

// SubOption configures a subscription created by Sub.
type SubOption func(*subscriber)

// WithQueueLimit sets the maximum number of events queued for the subscriber while its handler runs,
// DefaultQueueLimit without it. A limit <= 0 means no limit.
func WithQueueLimit(limit int) SubOption {
	return func(s *subscriber) {
		s.limit = limit
	}
}

// subscriber queues the events of one handler and calls it from its own goroutine.
type subscriber struct {
	hub     string
	handler EventFunc
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
					slog.String("hub", s.hub), slog.Int("dropped", dropped), slog.Int("limit", s.limit))
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
			slog.Error("events: handler panicked", slog.String("hub", s.hub), slog.Any("event", e),
				slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
		}
	}()
	s.handler(e)
}
