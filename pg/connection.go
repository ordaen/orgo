package pg

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Aliases of the pgx types used by Connection, so they can be used without importing pgx.
type (
	// Tx is a database transaction, it is passed to the model hooks.
	Tx = pgx.Tx
	// Rows is the result of Query.
	Rows = pgx.Rows
	// Row is the result of QueryRow.
	Row = pgx.Row
	// Batch is a set of queries sent with SendBatch.
	Batch = pgx.Batch
	// BatchResults are the results of SendBatch.
	BatchResults = pgx.BatchResults
	// CopyFromSource is the source of the rows of CopyFrom.
	CopyFromSource = pgx.CopyFromSource
	// Identifier is a table name of CopyFrom, like Identifier{"public", "users"}.
	Identifier = pgx.Identifier
	// PgError is an error returned by the PostgreSQL server, it is passed to DBErrorHandler.
	PgError = pgconn.PgError
)

// Connection is the query interface shared by GlobalConnection and Tx.
type Connection interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	SendBatch(ctx context.Context, b *Batch) BatchResults
	CopyFrom(ctx context.Context, tableName Identifier, columnNames []string, rowSrc CopyFromSource) (int64, error)
	Begin(ctx context.Context) (Tx, error)
}

var (
	_ Connection = (*GlobalConnection)(nil)
	_ Connection = (Tx)(nil)
)

// GlobalConnection is a Connection backed by a connection pool. Its methods return ErrNotConnected,
// or a Row or BatchResults failing with it, when it has no pool. It is safe to use while the pool is changed.
type GlobalConnection struct {
	pool   atomic.Pointer[pgxpool.Pool]
	logger atomic.Pointer[Logger]
}

// newGlobalConnection returns a GlobalConnection using pool.
func newGlobalConnection(pool *pgxpool.Pool) *GlobalConnection {
	c := &GlobalConnection{}
	c.pool.Store(pool)
	return c
}

// Connected reports whether the connection has a pool.
func (c *GlobalConnection) Connected() bool {
	return c.pool.Load() != nil
}

// Logger returns the Config.Logger of the connection, or slog.Default() when it is not set.
// The Logger is kept after Close, until the next Connect.
func (c *GlobalConnection) Logger() Logger {
	if l := c.logger.Load(); l != nil {
		return *l
	}
	return slog.Default()
}

// setLogger sets the logger returned by Logger, nil for slog.Default().
func (c *GlobalConnection) setLogger(l Logger) {
	if l == nil {
		c.logger.Store(nil)
		return
	}
	c.logger.Store(&l)
}

// Pool returns the underlying connection pool, for pool features not covered by Connection like Stat.
// It returns nil when the connection has no pool.
func (c *GlobalConnection) Pool() *pgxpool.Pool {
	return c.pool.Load()
}

// Exec runs sql, which does not return rows, on a pool connection.
func (c *GlobalConnection) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	pool := c.pool.Load()
	if pool == nil {
		return pgconn.CommandTag{}, ErrNotConnected
	}
	return pool.Exec(ctx, sql, args...)
}

// Query runs sql on a pool connection. The connection is released when the returned rows are closed.
func (c *GlobalConnection) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	pool := c.pool.Load()
	if pool == nil {
		return nil, ErrNotConnected
	}
	return pool.Query(ctx, sql, args...)
}

// QueryRow runs sql, which returns at most one row, on a pool connection.
// Errors are returned when the row is scanned.
func (c *GlobalConnection) QueryRow(ctx context.Context, sql string, args ...any) Row {
	pool := c.pool.Load()
	if pool == nil {
		return errRow{}
	}
	return pool.QueryRow(ctx, sql, args...)
}

// SendBatch sends the queries of b to the database in one round trip on a pool connection.
// The connection is released when the returned results are closed.
func (c *GlobalConnection) SendBatch(ctx context.Context, b *Batch) BatchResults {
	pool := c.pool.Load()
	if pool == nil {
		return errBatchResults{}
	}
	return pool.SendBatch(ctx, b)
}

// CopyFrom inserts the rows of rowSrc into the columns of the table with the COPY protocol
// and returns the number of inserted rows.
func (c *GlobalConnection) CopyFrom(ctx context.Context, tableName Identifier, columnNames []string, rowSrc CopyFromSource) (int64, error) {
	pool := c.pool.Load()
	if pool == nil {
		return 0, ErrNotConnected
	}
	return pool.CopyFrom(ctx, tableName, columnNames, rowSrc)
}

// Begin starts a transaction on a pool connection. The connection is released when the transaction ends.
func (c *GlobalConnection) Begin(ctx context.Context) (Tx, error) {
	pool := c.pool.Load()
	if pool == nil {
		return nil, ErrNotConnected
	}
	return pool.Begin(ctx)
}

// errRow is the Row of QueryRow without a pool.
type errRow struct{}

func (errRow) Scan(...any) error { return ErrNotConnected }

// errBatchResults are the BatchResults of SendBatch without a pool.
type errBatchResults struct{}

func (errBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, ErrNotConnected }
func (errBatchResults) Query() (Rows, error)             { return nil, ErrNotConnected }
func (errBatchResults) QueryRow() Row                    { return errRow{} }
func (errBatchResults) Close() error                     { return ErrNotConnected }
