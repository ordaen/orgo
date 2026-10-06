package pg

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	_ pgx.QueryTracer = (*queryLogger)(nil)
	_ pgx.BatchTracer = (*queryLogger)(nil)
)

// Logger logs the records of orgo, it is set by Config.Logger. *slog.Logger implements it.
type Logger interface {
	LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr)
}

// loggerOrDefault returns l, or slog.Default() when l is nil. slog.Default() is read on every call,
// so the default logger can be changed after Connect.
func loggerOrDefault(l Logger) Logger {
	if l == nil {
		return slog.Default()
	}
	return l
}

// queryLogger logs the queries of a connection pool with the logger, or slog.Default() when it is nil:
// failed queries with their SQL and error, and when debug is set, also the successful queries.
// The query args are not logged, they may contain secrets.
type queryLogger struct {
	debug  bool
	logger Logger
}

type queryStartKey struct{}

// queryStart is the query traced by TraceQueryStart, stored in the query context.
type queryStart struct {
	sql   string
	start time.Time
}

func (l *queryLogger) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{sql: data.SQL, start: time.Now()})
}

func (l *queryLogger) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	q, _ := ctx.Value(queryStartKey{}).(queryStart)
	l.log(ctx, q.sql, time.Since(q.start), data.CommandTag.String(), data.Err)
}

type batchStartKey struct{}

// batchTrace is the batch traced by TraceBatchStart, stored in the batch context.
type batchTrace struct {
	batch *pgx.Batch
	start time.Time
	// logged is set when an error of the batch was logged, so it is not logged again. The batch error is the error
	// of its failed query, and pgx may trace the end of a failed batch more than once.
	logged bool
}

// TraceBatchStart stores the batch, its queries are logged by TraceBatchQuery.
func (l *queryLogger) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	return context.WithValue(ctx, batchStartKey{}, &batchTrace{batch: data.Batch, start: time.Now()})
}

// TraceBatchQuery logs a query of the batch. The duration is the time since the batch started.
func (l *queryLogger) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	b := batchFrom(ctx)
	if data.Err != nil {
		b.logged = true
	}
	l.log(ctx, data.SQL, time.Since(b.start), data.CommandTag.String(), data.Err)
}

// TraceBatchEnd logs the error of a failed batch when none of its queries was logged as failed, like when a query
// fails to prepare. The failed query is not known then, so the SQL of all the batch queries is logged.
func (l *queryLogger) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	b := batchFrom(ctx)
	if data.Err == nil || b.logged {
		return
	}
	b.logged = true
	var sqls []string
	if b.batch != nil {
		for _, q := range b.batch.QueuedQueries {
			sqls = append(sqls, q.SQL)
		}
	}
	loggerOrDefault(l.logger).LogAttrs(ctx, slog.LevelError, "orgo: batch failed",
		slog.String("sql", strings.Join(sqls, "; ")), slog.Duration("duration", time.Since(b.start)),
		slog.Any("error", data.Err))
}

// batchFrom returns the batch traced in ctx.
func batchFrom(ctx context.Context) *batchTrace {
	if b, ok := ctx.Value(batchStartKey{}).(*batchTrace); ok {
		return b
	}
	return &batchTrace{}
}

func (l *queryLogger) log(ctx context.Context, sql string, d time.Duration, tag string, err error) {
	if err != nil {
		if h, ok := ctx.Value(heldQueryKey{}).(*heldQuery); ok && h.hold(l, ctx, sql, d, err) {
			return
		}
		l.logFailed(ctx, slog.LevelError, "orgo: query failed", sql, d, err)
		return
	}
	if l.debug {
		loggerOrDefault(l.logger).LogAttrs(ctx, slog.LevelInfo, "orgo: query",
			slog.String("sql", sql), slog.Duration("duration", d), slog.String("result", tag))
	}
}

// logFailed logs the failed query with its SQL and error.
func (l *queryLogger) logFailed(ctx context.Context, level slog.Level, msg, sql string, d time.Duration, err error) {
	loggerOrDefault(l.logger).LogAttrs(ctx, level, msg,
		slog.String("sql", sql), slog.Duration("duration", d), slog.Any("error", err))
}

type heldQueryKey struct{}

// heldQuery is the failed query of a model write, held until the DBErrorHandler of the model ran: a database error
// turned into an application error is the expected behavior of the model, not a failed query. See holdFailedQuery.
type heldQuery struct {
	logger *queryLogger // nil when no query is held
	ctx    context.Context
	sql    string
	d      time.Duration
	err    error
}

// holdFailedQuery returns ctx holding the failed query run with it, logged by release, when model has
// a DBErrorHandler. Otherwise it returns ctx and nil, and the failed queries are logged when they end.
func holdFailedQuery(ctx context.Context, model any) (context.Context, *heldQuery) {
	if _, ok := model.(DBErrorHandler); !ok {
		return ctx, nil
	}
	h := &heldQuery{}
	return context.WithValue(ctx, heldQueryKey{}, h), h
}

// hold keeps the failed query, it reports false when another one is held already.
func (h *heldQuery) hold(l *queryLogger, ctx context.Context, sql string, d time.Duration, err error) bool {
	if h.logger != nil {
		return false
	}
	*h = heldQuery{logger: l, ctx: ctx, sql: sql, d: d, err: err}
	return true
}

// release logs the held failed query: as a failed query, or when handled is set, at the debug level with Debug
// and not at all without it. It does nothing when no query is held, so it can be deferred and called again.
func (h *heldQuery) release(handled bool) {
	if h == nil || h.logger == nil {
		return
	}
	held := *h
	*h = heldQuery{}
	switch {
	case !handled:
		held.logger.logFailed(held.ctx, slog.LevelError, "orgo: query failed", held.sql, held.d, held.err)
	case held.logger.debug:
		held.logger.logFailed(held.ctx, slog.LevelDebug, "orgo: query failed, handled by the model", held.sql, held.d, held.err)
	}
}
