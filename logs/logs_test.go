package logs

import (
	"bytes"
	"context"
	"errors"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLog returns the buffer the standard logger writes to during the test.
func captureLog(t *testing.T) *bytes.Buffer {
	buf := new(bytes.Buffer)
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})
	return buf
}

// fakeTransport records the Sentry events instead of sending them.
type fakeTransport struct {
	mu      sync.Mutex
	events  []*sentry.Event
	flushed int
}

func (f *fakeTransport) Configure(sentry.ClientOptions) {}
func (f *fakeTransport) Close()                         {}
func (f *fakeTransport) SendEvent(e *sentry.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}
func (f *fakeTransport) Flush(time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed++
	return true
}
func (f *fakeTransport) FlushWithContext(context.Context) bool { return f.Flush(0) }

func (f *fakeTransport) Events() []*sentry.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*sentry.Event{}, f.events...)
}

// initTestSentry initializes Sentry with a fake transport, it is turned off at the end of the test.
func initTestSentry(t *testing.T) *fakeTransport {
	tr := &fakeTransport{}
	t.Setenv("SENTRY_DSN", "")
	require.NoError(t, InitSentry(SentryOptions{Dsn: "https://key@o0.ingest.sentry.io/1", Transport: tr}))
	t.Cleanup(func() {
		sentryOn.Store(false)
		sentry.CurrentHub().BindClient(nil)
	})
	return tr
}

func TestLog(t *testing.T) {
	buf := captureLog(t)
	Errorf("failed %d", 1)
	Error(errors.New("boom"), "failed")
	Debug("debug")
	Info("info")
	Infof("info %s", "f")
	assert.Equal(t, "[ERROR] failed 1\n[ERROR] failed boom\n[DEBUG] debug\n[INFO] info\n[INFO] info f\n", buf.String())
}

func TestInitSentryDisabled(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")
	require.NoError(t, InitSentry(SentryOptions{}))
	assert.False(t, SentryOn())
	assert.True(t, FlushSentry(time.Millisecond))
}

func TestInitSentryInvalidDSN(t *testing.T) {
	t.Setenv("SENTRY_DSN", "not a dsn")
	assert.Error(t, InitSentry(SentryOptions{}))
	assert.False(t, SentryOn())
}

func TestInitSentry(t *testing.T) {
	captureLog(t)
	tr := initTestSentry(t)
	assert.True(t, SentryOn())

	CaptureException(errors.New("captured"))
	require.Len(t, tr.Events(), 1)
	assert.Equal(t, "captured", tr.Events()[0].Exception[0].Value)
	assert.True(t, FlushSentry(time.Second))
	assert.Equal(t, 1, tr.flushed)
}

func TestRecoverWithoutSentry(t *testing.T) {
	buf := captureLog(t)
	var got any
	func() {
		defer Recover(func(err any) { got = err })
		panic("boom")
	}()
	assert.Equal(t, "boom", got)
	assert.Contains(t, buf.String(), "[ERROR] panic: boom")
	assert.Contains(t, buf.String(), "logs_test.go", "the stack trace is logged")
}

func TestRecoverWithSentry(t *testing.T) {
	captureLog(t)
	tr := initTestSentry(t)
	func() {
		defer Recover()
		panic("boom")
	}()
	require.Len(t, tr.Events(), 1)
	assert.Equal(t, sentry.LevelFatal, tr.Events()[0].Level)
}

func TestRecoverNoPanic(t *testing.T) {
	buf := captureLog(t)
	called := false
	func() {
		defer Recover(func(any) { called = true })
	}()
	assert.False(t, called)
	assert.Empty(t, buf.String())
}

func TestSentryWithScope(t *testing.T) {
	fallback := false
	SentryWithScope(func(*Scope) { t.Error("scope called without Sentry") }, func() { fallback = true })
	assert.True(t, fallback)

	captureLog(t)
	initTestSentry(t)
	called := false
	SentryWithScope(func(*Scope) { called = true }, func() { t.Error("fallback called with Sentry") })
	assert.True(t, called)
}

func TestHub(t *testing.T) {
	h := &hubs{hub: make(map[string]*sentry.Hub)}
	var wg sync.WaitGroup
	got := make([]*sentry.Hub, 16)
	for i := range got {
		wg.Go(func() { got[i] = h.Hub("name") })
	}
	wg.Wait()
	for _, hub := range got {
		assert.Same(t, got[0], hub, "concurrent first calls return the same hub")
	}
	assert.NotSame(t, got[0], h.Hub("other"))
	assert.Same(t, sentry.CurrentHub(), h.CurrentHub())
}
