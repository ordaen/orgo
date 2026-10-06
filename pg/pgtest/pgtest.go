// Package pgtest helps the tests using the global pg.DB.
package pgtest

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ordaen/orgo/pg"
)

// Synctest runs f in a synctest bubble like synctest.Test, after opening all the connections of the pool
// of pg.DB outside of it. The pool must not open or close a connection in a bubble: its WaitGroup is shared
// with the code outside, which is a fatal error. With all its connections open, a query in the bubble waits
// for a free connection instead, so the tests set a small MaxConns. A query in the bubble must not break its
// connection, like by canceling the query, the pool would close it.
func Synctest(t *testing.T, f func(*testing.T)) {
	t.Helper()
	OpenConns(t)
	synctest.Test(t, f)
}

// OpenConns opens all the connections of the pool of pg.DB, up to its MaxConns.
func OpenConns(t testing.TB) {
	t.Helper()
	pool := pg.DB.Pool()
	if pool == nil {
		t.Fatal("pgtest: pg.DB is not connected")
	}
	conns := make([]*pgxpool.Conn, 0, pool.Config().MaxConns)
	defer func() {
		for _, c := range conns {
			c.Release()
		}
	}()
	for range pool.Config().MaxConns {
		c, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("pgtest: open connection: %v", err)
		}
		conns = append(conns, c)
	}
}
