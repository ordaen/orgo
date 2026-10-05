package pg

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDSN(t *testing.T) {
	cfg := Config{Username: "user@corp", Password: "p@ss:w/rd?#", Host: "localhost:5432", DatabaseName: "my db"}

	u, err := url.Parse(cfg.DSN())
	require.NoError(t, err)
	assert.Equal(t, "postgres", u.Scheme)
	assert.Equal(t, "user@corp", u.User.Username())
	password, _ := u.User.Password()
	assert.Equal(t, "p@ss:w/rd?#", password)
	assert.Equal(t, "localhost:5432", u.Host)
	assert.Equal(t, "/my db", u.Path)
	assert.Empty(t, u.RawQuery)

	// the params are escaped in the query, the zero ones are left out
	cfg.Params = ConnectionParams{
		SSLMode:          "verify-full",
		SSLRootCert:      "/etc/ssl/ca.pem",
		ApplicationName:  "my app&co",
		ConnectTimeout:   1500 * time.Millisecond,
		StatementTimeout: 2500 * time.Microsecond,
	}
	u, err = url.Parse(cfg.DSN())
	require.NoError(t, err)
	assert.Equal(t, url.Values{
		"sslmode":           {"verify-full"},
		"sslrootcert":       {"/etc/ssl/ca.pem"},
		"application_name":  {"my app&co"},
		"connect_timeout":   {"2"},
		"statement_timeout": {"3"},
	}, u.Query(), "the timeouts are rounded up")
	assert.Equal(t, "sslmode=verify-full&sslrootcert=%2Fetc%2Fssl%2Fca.pem&connect_timeout=2&application_name=my%20app%26co&statement_timeout=3",
		u.RawQuery, "spaces are escaped as %20")
}

func TestConnectWithPoolParams(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()
	cfg.Params = ConnectionParams{
		SSLMode:          "disable",
		ConnectTimeout:   5 * time.Second,
		ApplicationName:  "orgo test",
		StatementTimeout: 1500 * time.Millisecond,
	}
	pool, err := ConnectWithPool(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()

	connCfg := pool.Config().ConnConfig
	assert.Nil(t, connCfg.TLSConfig, "sslmode=disable turns TLS off")
	assert.Equal(t, 5*time.Second, connCfg.ConnectTimeout)
	var appName, statementTimeout string
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_setting('application_name'), current_setting('statement_timeout')").
		Scan(&appName, &statementTimeout))
	assert.Equal(t, "orgo test", appName)
	assert.Equal(t, "1500ms", statementTimeout)

	// the statement timeout aborts the longer statements
	_, err = pool.Exec(ctx, "SELECT pg_sleep(2)")
	require.ErrorContains(t, err, "canceling statement due to statement timeout")

	// invalid params fail the connect
	cfg.Params = ConnectionParams{SSLMode: "bogus"}
	_, err = ConnectWithPool(ctx, cfg)
	require.ErrorContains(t, err, "[PG] configuration: ")
}

func TestConnectWithPoolDefaults(t *testing.T) {
	ctx := context.Background()

	pool, err := ConnectWithPool(ctx, testConfig())
	require.NoError(t, err)
	defer pool.Close()
	cfg := pool.Config()
	assert.Equal(t, int32(defaultMaxConns), cfg.MaxConns)
	assert.Equal(t, int32(defaultMinConns), cfg.MinConns)
	assert.Equal(t, defaultMaxConnLifetime, cfg.MaxConnLifetime)
	assert.Equal(t, defaultMaxConnIdleTime, cfg.MaxConnIdleTime)

	// MinConns default must not exceed a lower MaxConns
	c := testConfig()
	c.MaxConns = 2
	c.MaxConnIdleTime = time.Minute
	pool2, err := ConnectWithPool(ctx, c)
	require.NoError(t, err)
	defer pool2.Close()
	assert.Equal(t, int32(2), pool2.Config().MaxConns)
	assert.Equal(t, int32(2), pool2.Config().MinConns)
	assert.Equal(t, time.Minute, pool2.Config().MaxConnIdleTime)
}

func TestConnectWithPoolSchema(t *testing.T) {
	ctx := context.Background()
	const schema = "orgo Test-Schema" // needs quoting
	_, err := DB.Exec(ctx, `DROP SCHEMA IF EXISTS "orgo Test-Schema" CASCADE`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := DB.Exec(context.Background(), `DROP SCHEMA IF EXISTS "orgo Test-Schema" CASCADE`)
		assert.NoError(t, err)
	})

	cfg := testConfig()
	cfg.Schema = schema
	cfg.MaxConns = 2
	pool, err := ConnectWithPool(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()

	// the schema is created and used by every connection of the pool
	for range 2 {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		var current string
		require.NoError(t, conn.QueryRow(ctx, "SELECT current_schema()").Scan(&current))
		assert.Equal(t, schema, current)
	}
}

func TestConnectAlreadyConnected(t *testing.T) {
	require.ErrorIs(t, Connect(testConfig()), ErrDatabaseAlreadyConnected)
	assert.NotNil(t, DB.Pool())
}

func TestCloseAndReconnect(t *testing.T) {
	Close()
	require.False(t, DB.Connected())
	assert.Nil(t, DB.Pool())
	Close() // closing twice is a no-op

	// a failed connect leaves DB not connected, so it can be retried
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, ConnectWithContext(ctx, testConfig()), context.Canceled)
	require.False(t, DB.Connected())

	require.NoError(t, Connect(testConfig()))
	require.True(t, DB.Connected())
	var one int
	require.NoError(t, DB.QueryRow(context.Background(), "SELECT 1").Scan(&one))
	assert.Equal(t, 1, one)
}

func TestNotConnected(t *testing.T) {
	Close()
	t.Cleanup(func() { require.NoError(t, Connect(testConfig())) })
	ctx := context.Background()

	_, err := DB.Exec(ctx, "SELECT 1")
	require.ErrorIs(t, err, ErrNotConnected)
	_, err = DB.Query(ctx, "SELECT 1")
	require.ErrorIs(t, err, ErrNotConnected)
	var one int
	require.ErrorIs(t, DB.QueryRow(ctx, "SELECT 1").Scan(&one), ErrNotConnected)
	_, err = DB.Begin(ctx)
	require.ErrorIs(t, err, ErrNotConnected)
	_, err = DB.CopyFrom(ctx, Identifier{"base_models"}, []string{"name"}, pgx.CopyFromRows(nil))
	require.ErrorIs(t, err, ErrNotConnected)
	b := &Batch{}
	b.Queue("SELECT 1")
	res := DB.SendBatch(ctx, b)
	_, err = res.Exec()
	require.ErrorIs(t, err, ErrNotConnected)
	_, err = res.Query()
	require.ErrorIs(t, err, ErrNotConnected)
	require.ErrorIs(t, res.QueryRow().Scan(&one), ErrNotConnected)
	require.ErrorIs(t, res.Close(), ErrNotConnected)

	// the functions using DB return the error instead of panicking
	_, err = CreateModel(&baseModel{Name: "john"})
	require.ErrorIs(t, err, ErrNotConnected)
	_, err = Query(&baseModel{}).Select()
	require.ErrorIs(t, err, ErrNotConnected)
	_, err = CountWithContext(ctx, "base_models")
	require.ErrorIs(t, err, ErrNotConnected)
	require.ErrorIs(t, ClearTables("base_models"), ErrNotConnected)
}

func TestConnectCloseConcurrentQueries(t *testing.T) {
	t.Cleanup(func() {
		if !DB.Connected() {
			require.NoError(t, Connect(testConfig()))
		}
	})
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 20 {
				// a query succeeds or fails with ErrNotConnected, it never panics
				// a query that got the pool just before Close fails on the closed pool
				if _, err := DB.Exec(ctx, "SELECT 1"); err != nil && !errors.Is(err, ErrNotConnected) {
					assert.ErrorContains(t, err, "closed pool")
				}
			}
		})
	}
	for range 3 {
		Close()
		require.NoError(t, Connect(testConfig()))
	}
	wg.Wait()
}

func TestScanInetIntoString(t *testing.T) {
	ctx := context.Background()
	_, err := DB.Exec(ctx, `DROP TABLE IF EXISTS inet_scans; CREATE TABLE inet_scans (ip Inet, net Cidr, empty Cidr)`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = DB.Exec(ctx, `DROP TABLE inet_scans`) })

	// strings are accepted as arguments too
	_, err = DB.Exec(ctx, `INSERT INTO inet_scans (ip, net) VALUES ($1, $2)`, "10.0.0.1", "192.168.0.0/16")
	require.NoError(t, err)

	var ip, net string
	var empty *string
	require.NoError(t, DB.QueryRow(ctx, `SELECT ip, net, empty FROM inet_scans`).Scan(&ip, &net, &empty))
	assert.Equal(t, "10.0.0.1", ip)
	assert.Equal(t, "192.168.0.0/16", net)
	assert.Nil(t, empty)

	// the netip types still work
	var addr netip.Addr
	var prefix netip.Prefix
	require.NoError(t, DB.QueryRow(ctx, `SELECT ip, net FROM inet_scans`).Scan(&addr, &prefix))
	assert.Equal(t, netip.MustParseAddr("10.0.0.1"), addr)
	assert.Equal(t, netip.MustParsePrefix("192.168.0.0/16"), prefix)
}
