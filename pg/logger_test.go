package pg

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logRecord is a record logged by the query logger, decoded from the JSON handler output.
type logRecord struct {
	Level  string `json:"level"`
	Msg    string `json:"msg"`
	SQL    string `json:"sql"`
	Error  string `json:"error"`
	Result string `json:"result"`
}

// captureLogs sets slog.Default() to a JSON logger until the test ends and returns a function
// returning the records logged since the previous call.
func captureLogs(t *testing.T) func() []logRecord {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []logRecord {
		var res []logRecord
		for line := range strings.Lines(buf.String()) {
			var r logRecord
			require.NoError(t, json.Unmarshal([]byte(line), &r))
			res = append(res, r)
		}
		buf.Reset()
		return res
	}
}

// queryPool returns a pool connected with the test configuration and debug, closed when the test ends.
func queryPool(t *testing.T, debug bool) *GlobalConnection {
	t.Helper()
	cfg := testConfig()
	cfg.Debug = debug
	pool, err := ConnectWithPool(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return newGlobalConnection(pool)
}

func TestQueryLoggerFailedQueries(t *testing.T) {
	logs := captureLogs(t)
	conn := queryPool(t, false)
	ctx := context.Background()
	logs() // the connect queries

	// successful queries are not logged without Debug
	_, err := conn.Exec(ctx, "SELECT 1")
	require.NoError(t, err)
	var n int
	require.NoError(t, conn.QueryRow(ctx, "SELECT $1::int", 1).Scan(&n))
	assert.Empty(t, logs())

	// no rows is not a failed query
	err = conn.QueryRow(ctx, "SELECT 1 WHERE false").Scan(&n)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	assert.Empty(t, logs())

	_, err = conn.Exec(ctx, "SELECT * FROM missing_table WHERE id = $1", 42)
	require.Error(t, err)
	rows, err := conn.Query(ctx, "SELECT missing_column FROM base_models")
	if err == nil {
		rows.Close()
		err = rows.Err()
	}
	require.Error(t, err)

	records := logs()
	require.Len(t, records, 2)
	assert.Equal(t, "ERROR", records[0].Level)
	assert.Equal(t, "orgo: query failed", records[0].Msg)
	assert.Equal(t, "SELECT * FROM missing_table WHERE id = $1", records[0].SQL)
	assert.Contains(t, records[0].Error, `relation "missing_table" does not exist`)
	assert.Equal(t, "SELECT missing_column FROM base_models", records[1].SQL)
	assert.Contains(t, records[1].Error, `column "missing_column" does not exist`)
}

func TestQueryLoggerDebug(t *testing.T) {
	logs := captureLogs(t)
	conn := queryPool(t, true)
	ctx := context.Background()
	logs()

	_, err := conn.Exec(ctx, "SELECT 1")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "SELECT * FROM missing_table")
	require.Error(t, err)

	records := logs()
	require.Len(t, records, 2)
	assert.Equal(t, logRecord{Level: "INFO", Msg: "orgo: query", SQL: "SELECT 1", Result: "SELECT 1"}, records[0])
	assert.Equal(t, "ERROR", records[1].Level)
	assert.Equal(t, "SELECT * FROM missing_table", records[1].SQL)
}

func TestQueryLoggerBatch(t *testing.T) {
	logs := captureLogs(t)
	conn := queryPool(t, true)
	ctx := context.Background()
	logs()

	// the division fails when it runs, a query failing to prepare would fail the whole batch before it runs
	b := &Batch{}
	b.Queue("SELECT 1")
	b.Queue("SELECT 1 / $1::int", 0)
	res := conn.SendBatch(ctx, b)
	_, err := res.Exec()
	assert.NoError(t, err)
	_, err = res.Exec()
	assert.ErrorContains(t, err, "division by zero")
	assert.Error(t, res.Close()) // the connection must be released before the pool is closed

	records := logs()
	require.Len(t, records, 2)
	assert.Equal(t, logRecord{Level: "INFO", Msg: "orgo: query", SQL: "SELECT 1", Result: "SELECT 1"}, records[0])
	assertFailedQuery(t, records[1], "SELECT 1 / $1::int", "division by zero")

	// a batch failing to prepare is logged once with all its queries
	b = &Batch{}
	b.Queue("SELECT 1")
	b.Queue("SELECT * FROM missing_table")
	assert.Error(t, conn.SendBatch(ctx, b).Close())
	records = logs()
	require.Len(t, records, 1)
	assert.Equal(t, "orgo: batch failed", records[0].Msg)
	assert.Equal(t, "SELECT 1; SELECT * FROM missing_table", records[0].SQL)
	assert.Contains(t, records[0].Error, `relation "missing_table" does not exist`)
}

func TestQueryLoggerArgsNotLogged(t *testing.T) {
	logs := captureLogs(t)
	conn := queryPool(t, true)
	logs()

	_, err := conn.Exec(context.Background(), "SELECT $1::text", "secret-password")
	require.NoError(t, err)
	records := logs()
	require.Len(t, records, 1)
	raw, err := json.Marshal(records)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "secret-password")
}

// assertFailedQuery checks that r is a failed query record of sql with an error containing errText.
// The failed batch queries are logged as failed batches when pgx does not trace them separately.
func assertFailedQuery(t *testing.T, r logRecord, sql, errText string) {
	t.Helper()
	assert.Equal(t, "ERROR", r.Level)
	if r.Msg == "orgo: batch failed" {
		assert.Contains(t, r.SQL, sql)
	} else {
		assert.Equal(t, "orgo: query failed", r.Msg)
		assert.Equal(t, sql, r.SQL)
	}
	assert.Contains(t, r.Error, errText)
}

func TestQueryLoggerConfigLogger(t *testing.T) {
	logs := captureLogs(t)
	var buf bytes.Buffer
	cfg := testConfig()
	cfg.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	pool, err := ConnectWithPool(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = newGlobalConnection(pool).Exec(context.Background(), "SELECT * FROM missing_table")
	require.Error(t, err)

	// the failed query is logged with the Config.Logger, not slog.Default()
	assert.Contains(t, buf.String(), `"msg":"orgo: query failed","sql":"SELECT * FROM missing_table"`)
	assert.Empty(t, logs())
}

func TestGlobalConnectionLogger(t *testing.T) {
	c := &GlobalConnection{}
	assert.Same(t, slog.Default(), c.Logger())

	// the default logger is read on every call
	logs := captureLogs(t)
	c.Logger().LogAttrs(context.Background(), slog.LevelError, "default")
	require.Len(t, logs(), 1)

	l := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	c.setLogger(l)
	assert.Same(t, l, c.Logger())
	c.setLogger(nil)
	assert.Same(t, slog.Default(), c.Logger())
}

func TestConnectSetsLogger(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg := testConfig()
	cfg.Logger = l
	Close()
	t.Cleanup(func() {
		Close()
		require.NoError(t, Connect(testConfig()))
		assert.Same(t, slog.Default(), DB.Logger(), "a connect without Logger uses slog.Default()")
	})
	require.NoError(t, Connect(cfg))
	assert.Same(t, l, DB.Logger())

	_, err := DB.Exec(context.Background(), "SELECT * FROM missing_table")
	require.Error(t, err)
	assert.Contains(t, buf.String(), `"msg":"orgo: query failed"`)
}
