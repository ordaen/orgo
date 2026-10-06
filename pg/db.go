// Package pg connects to PostgreSQL, migrates the registered schema, and inserts, updates, deletes and queries
// models, structs mapped to table rows by their fields. The repo package builds repositories on top of it.
package pg

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ordaen/orgo/model"
)

// Model is model.Model, so it can be used without importing the model package.
type Model = model.Model

// DB is the global connection. Connect sets its pool and Close removes it, before Connect and after Close
// its queries return ErrNotConnected. DB itself is never nil, use DB.Connected to check the connection.
var DB = &GlobalConnection{}

// connectMu serializes Connect and Close, which change DB.
var connectMu sync.Mutex

// Pool defaults used for zero Config values.
const (
	defaultMaxConns        = 20
	defaultMinConns        = 5
	defaultMaxConnLifetime = 30 * time.Minute
	defaultMaxConnIdleTime = 5 * time.Minute
)

// Config is the configuration of the connection pool. A zero pool setting uses its default.
type Config struct {
	// DatabaseName is the database to connect to, Username and Password are its credentials.
	DatabaseName string
	Username     string
	Password     string
	// Host is the database host, optionally with the port: "localhost:5432".
	Host string
	// Params are the connection parameters added to the DSN.
	Params ConnectionParams
	// MaxConns defaults to 20.
	MaxConns int32
	// MinConns defaults to 5, or to MaxConns when it is lower.
	MinConns int32
	// MaxConnLifetime defaults to 30 minutes.
	MaxConnLifetime time.Duration
	// MaxConnIdleTime defaults to 5 minutes.
	MaxConnIdleTime time.Duration
	// Schema is created if it does not exist and set as the search_path of every connection.
	Schema string
	// AllowTableDrops allows DROP TABLE and DROP FOREIGN TABLE in the registered schema queries and functions.
	AllowTableDrops bool
	// Debug logs all queries with the Logger. Without it only the failed queries are logged, except the failed
	// writes handled by the DBErrorHandler of their model, which Debug logs at the debug level.
	// The query args are never logged.
	Debug bool
	// Logger logs the queries and the errors of the repositories using the global DB. When it is nil,
	// slog.Default() is used.
	Logger Logger
}

// ConnectionParams are the connection parameters of Config. Zero values are not added to the DSN,
// so the pgx and server defaults apply.
type ConnectionParams struct {
	// SSLMode is the TLS mode: "disable", "allow", "prefer", "require", "verify-ca" or "verify-full".
	// pgx defaults to "prefer".
	SSLMode string
	// SSLRootCert is the path of the certificate authorities file verifying the server certificate.
	SSLRootCert string
	// SSLCert and SSLKey are the paths of the client certificate and its key, SSLPassword decrypts the key.
	SSLCert     string
	SSLKey      string
	SSLPassword string
	// ConnectTimeout is the timeout of a connection attempt, rounded up to seconds.
	ConnectTimeout time.Duration
	// ApplicationName is shown in pg_stat_activity and the server logs.
	ApplicationName string
	// StatementTimeout aborts the statements running longer, rounded up to milliseconds.
	StatementTimeout time.Duration
}

// query returns the params as a DSN query, in a fixed order.
func (p *ConnectionParams) query() string {
	var params []string
	add := func(name, value string) {
		if value != "" {
			params = append(params, name+"="+queryEscape(value))
		}
	}
	add("sslmode", p.SSLMode)
	add("sslrootcert", p.SSLRootCert)
	add("sslcert", p.SSLCert)
	add("sslkey", p.SSLKey)
	add("sslpassword", p.SSLPassword)
	add("connect_timeout", ceilDuration(p.ConnectTimeout, time.Second))
	add("application_name", p.ApplicationName)
	add("statement_timeout", ceilDuration(p.StatementTimeout, time.Millisecond))
	return strings.Join(params, "&")
}

// ceilDuration returns d in units rounded up, or an empty string when d is not positive.
func ceilDuration(d, unit time.Duration) string {
	if d <= 0 {
		return ""
	}
	return strconv.FormatInt(int64((d+unit-1)/unit), 10)
}

// DSN returns the connection URL with the Params as its query. The credentials, the database name and the Params
// are escaped.
func (c *Config) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.Username, c.Password),
		Host:     c.Host,
		Path:     "/" + c.DatabaseName,
		RawQuery: c.Params.query(),
	}
	return u.String()
}

// queryEscape escapes a DSN query parameter. Spaces are escaped as %20, pgx does not decode + as a space.
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// Connect is like ConnectWithContext with context.Background().
func Connect(cfg Config) error {
	return ConnectWithContext(context.Background(), cfg)
}

// ConnectWithContext creates the connection pool, saves it in the global DB, migrates the registered schema,
// sets up the registered repositories and runs the registered init functions. When one of them fails,
// DB is not connected.
// It returns ErrDatabaseAlreadyConnected when DB is already connected.
// Use ConnectWithPool to get a pool without the global DB.
func ConnectWithContext(ctx context.Context, cfg Config) error {
	connectMu.Lock()
	defer connectMu.Unlock()

	if DB.Connected() {
		return ErrDatabaseAlreadyConnected
	}

	pool, err := ConnectWithPool(ctx, cfg)
	if err != nil {
		return err
	}
	DB.setLogger(cfg.Logger)
	DB.pool.Store(pool)

	if err := migrateSchema(ctx, cfg.AllowTableDrops); err != nil {
		closeHalfConnected()
		return err
	}
	if err := setupRepositories(); err != nil {
		closeHalfConnected()
		return err
	}
	if err := runInits(); err != nil {
		closeHalfConnected()
		return err
	}
	return nil
}

// closeHalfConnected closes a connection that failed to initialize, so Connect can be retried.
func closeHalfConnected() {
	DB.pool.Swap(nil).Close()
}

// Close closes the global DB pool. Connect can be called again after it.
func Close() {
	connectMu.Lock()
	defer connectMu.Unlock()

	// queries started after the swap return ErrNotConnected, Close waits for the running ones
	if pool := DB.pool.Swap(nil); pool != nil {
		pool.Close()
	}
}

// ConnectWithPool creates a new connection pool, creates the schema if it is set and pings the database.
// The pool logs its failed queries, or all queries when cfg.Debug is set, with cfg.Logger.
// It does not change the global DB.
func ConnectWithPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, Errorf("configuration: %w", err)
	}

	if cfg.Schema != "" {
		// sent with the connection startup, no extra query per connection
		poolConfig.ConnConfig.RuntimeParams["search_path"] = QuoteIdent(cfg.Schema)
	}
	poolConfig.ConnConfig.Tracer = &queryLogger{debug: cfg.Debug, logger: cfg.Logger}
	poolConfig.AfterConnect = registerTextTypes

	poolConfig.MaxConns = defaultIfZero(cfg.MaxConns, defaultMaxConns)
	poolConfig.MinConns = defaultIfZero(cfg.MinConns, min(defaultMinConns, poolConfig.MaxConns))
	poolConfig.MaxConnLifetime = defaultIfZero(cfg.MaxConnLifetime, defaultMaxConnLifetime)
	poolConfig.MaxConnIdleTime = defaultIfZero(cfg.MaxConnIdleTime, defaultMaxConnIdleTime)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, Errorf("connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, Errorf("ping: %w", err)
	}

	if cfg.Schema != "" {
		if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+QuoteIdent(cfg.Schema)); err != nil {
			pool.Close()
			return nil, Errorf("create schema %s: %w", cfg.Schema, err)
		}
	}

	return pool, nil
}

// defaultIfZero returns def when v is zero or negative.
func defaultIfZero[T int32 | time.Duration](v, def T) T {
	if v > 0 {
		return v
	}
	return def
}

// textInetCodec is pgtype.InetCodec reading inet and cidr in text format. In binary format pgx scans them
// only into netip.Prefix and netip.Addr, in text format they can also be scanned into a string.
type textInetCodec struct{ pgtype.InetCodec }

func (textInetCodec) PreferredFormat() int16 { return pgtype.TextFormatCode }

// registerTextTypes makes the connection read inet and cidr in text format, so they can be scanned into string fields.
func registerTextTypes(_ context.Context, conn *pgx.Conn) error {
	m := conn.TypeMap()
	m.RegisterType(&pgtype.Type{Name: "inet", OID: pgtype.InetOID, Codec: textInetCodec{}})
	m.RegisterType(&pgtype.Type{Name: "cidr", OID: pgtype.CIDROID, Codec: textInetCodec{}})
	return nil
}
