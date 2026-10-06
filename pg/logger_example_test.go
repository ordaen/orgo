package pg_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is an example of an external logger set by pg.Config.Logger. DBLogger keeps the logged errors
// with their failed queries instead of writing them, an application logger would send them to its logging system.

// testDbError is an error logged by orgo.
type testDbError struct {
	Error string
	Query string
}

// DBLogger implements pg.Logger. orgo calls it from the goroutines running the queries, so it is locked.
type DBLogger struct {
	mu     sync.Mutex
	Errors []testDbError
}

var _ pg.Logger = (*DBLogger)(nil)

// LogAttrs keeps the error records. orgo logs the failed queries with the "sql" and "error" attributes,
// the repository errors with the "table" and "error" attributes, and with Config.Debug the successful queries
// at slog.LevelInfo.
func (l *DBLogger) LogAttrs(_ context.Context, level slog.Level, _ string, attrs ...slog.Attr) {
	if level < slog.LevelError {
		return
	}
	var e testDbError
	for _, a := range attrs {
		switch a.Key {
		case "sql":
			e.Query = a.Value.String()
		case "error":
			e.Error = a.Value.String()
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.Errors = append(l.Errors, e)
}

func TestExternalLogger(t *testing.T) {
	logger := &DBLogger{}
	cfg := pg.TestConfig()
	cfg.Logger = logger

	// the logger is set on Connect
	pg.Close()
	t.Cleanup(func() {
		pg.Close()
		require.NoError(t, pg.Connect(pg.TestConfig()))
	})
	require.NoError(t, pg.Connect(cfg))

	_, err := pg.Exec("SELECT * FROM missing_table")
	require.Error(t, err)

	logger.mu.Lock()
	defer logger.mu.Unlock()
	require.Len(t, logger.Errors, 1)
	assert.Equal(t, "SELECT * FROM missing_table", logger.Errors[0].Query)
	assert.Contains(t, logger.Errors[0].Error, `relation "missing_table" does not exist`)
}
