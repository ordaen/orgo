package logs

import (
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/ordaen/orgo/env"
)

type (
	Scope         = sentry.Scope
	SentryOptions = sentry.ClientOptions
)

const (
	SentryLevelDebug   = sentry.LevelDebug
	SentryLevelInfo    = sentry.LevelInfo
	SentryLevelWarning = sentry.LevelWarning
	SentryLevelError   = sentry.LevelError
	SentryLevelFatal   = sentry.LevelFatal
)

// flushTimeout is the maximum time Fatal and Fatalf wait for the buffered Sentry events to be sent.
const flushTimeout = 2 * time.Second

var sentryOn atomic.Bool

// SentryOn reports whether Sentry is initialized by InitSentry.
func SentryOn() bool {
	return sentryOn.Load()
}

// InitSentry initializes Sentry with the options. The DSN is the SENTRY_DSN environment variable, or opts.Dsn
// when it is not set. Sentry is not initialized when both are empty, it is not an error.
func InitSentry(opts SentryOptions) error {
	opts.Dsn = env.Get("SENTRY_DSN", opts.Dsn)
	if opts.Dsn == "" {
		return nil
	}

	if err := sentry.Init(opts); err != nil {
		return fmt.Errorf("failed to initialize Sentry: %w", err)
	}
	sentryOn.Store(true)
	Info("Initialized Sentry")
	return nil
}

// FlushSentry waits until the buffered Sentry events are sent or the timeout is reached,
// it should be called before the program exits. It returns false when the timeout was reached.
func FlushSentry(timeout time.Duration) bool {
	if !SentryOn() {
		return true
	}
	return sentry.Flush(timeout)
}

func flushSentry() {
	FlushSentry(flushTimeout)
}

// Recover recovers a panic and reports it to Sentry, or logs it with the stack trace when Sentry is not
// initialized. The extras are called with the panic value. It must be deferred directly, recover does not
// stop a panic when it is called by a function called by the deferred function:
//
//	defer logs.Recover()
func Recover(extras ...func(err any)) {
	r := recover()
	if r == nil {
		return
	}
	if SentryOn() {
		sentry.CurrentHub().Recover(r)
	} else {
		Errorf("panic: %v\n%s", r, debug.Stack())
	}
	for _, f := range extras {
		f(r)
	}
}

// SentryWithScope calls scopeFunc with a new Sentry scope, or the fallBacks when Sentry is not initialized.
func SentryWithScope(scopeFunc func(scope *Scope), fallBacks ...func()) {
	if SentryOn() {
		sentry.WithScope(scopeFunc)
		return
	}
	for _, f := range fallBacks {
		f()
	}
}

// CaptureException reports the error to Sentry, it does nothing when Sentry is not initialized.
func CaptureException(err error) {
	sentry.CaptureException(err)
}

// Sentry holds the named Sentry hubs
var Sentry = &hubs{
	hub: make(map[string]*sentry.Hub),
}

type hubs struct {
	sync.RWMutex
	hub map[string]*sentry.Hub
}

// CurrentHub returns the current Sentry hub
func (h *hubs) CurrentHub() *sentry.Hub {
	return sentry.CurrentHub()
}

// Hub returns the named hub, creating it as a clone of the current hub on the first call.
// The clone has the client of the current hub at that time, so the hubs should be created after InitSentry.
func (h *hubs) Hub(name string) *sentry.Hub {
	h.RLock()
	hub := h.hub[name]
	h.RUnlock()
	if hub != nil {
		return hub
	}

	h.Lock()
	defer h.Unlock()
	// another caller may have created it since the read lock was released
	if hub = h.hub[name]; hub == nil {
		hub = sentry.CurrentHub().Clone()
		h.hub[name] = hub
	}
	return hub
}
