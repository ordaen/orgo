package pg

import (
	"context"
	"embed"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/schema
var schemaFiles embed.FS

// dropSchemaTables drops the tables created by the schema tests, now and when the test ends.
func dropSchemaTables(t *testing.T) {
	t.Helper()
	drop := func() {
		_, err := DB.Exec(context.Background(), `DROP TABLE IF EXISTS schema_children, schema_parents, tt1 CASCADE;
			DROP FUNCTION IF EXISTS schema_children_count`, pgx.QueryExecModeSimpleProtocol)
		require.NoError(t, err)
	}
	drop()
	t.Cleanup(drop)
}

// tableExists reports whether the table exists in the test schema.
func tableExists(t *testing.T, table string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, DB.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists))
	return exists
}

func TestCreateSchema(t *testing.T) {
	dropSchemaTables(t)
	s := &schema{}
	s.Register("tt1",
		`create table if not exists tt1();`,
		`DROP TABLE IF EXISTS tt1;`,
	)
	assert.EqualError(t, s.migrate(context.Background(), false),
		"[PG] migrate table tt1: DROP TABLE is not allowed: [DROP TABLE IF EXISTS tt1;]")
	assert.False(t, tableExists(t, "tt1"), "nothing runs when a query is not allowed")
	assert.NoError(t, s.migrate(context.Background(), true))
}

func TestSchemaMigrateInRegistrationOrder(t *testing.T) {
	ctx := context.Background()
	// the child references the parent, so it fails when it runs first
	for range 10 {
		dropSchemaTables(t)
		s := &schema{}
		s.Register("schema_parents", `CREATE TABLE schema_parents ("id" Bigserial PRIMARY KEY)`)
		s.Register("schema_children", `CREATE TABLE schema_children ("parent_id" Bigint REFERENCES schema_parents ("id"))`)
		require.NoError(t, s.migrate(ctx, false))
	}

	// registering a table again replaces its queries and keeps its position
	s := &schema{}
	s.Register("schema_parents", `SELECT 1`)
	s.Register("schema_children", `SELECT 2`)
	s.Register("schema_parents", `SELECT 3`)
	assert.Equal(t, []string{"schema_parents", "schema_children"}, s.ListTables())
	assert.Equal(t, []string{"SELECT 3"}, s.tables[0].queries)
}

func TestSchemaMigrateRollsBack(t *testing.T) {
	dropSchemaTables(t)
	s := &schema{}
	s.RegisterFunction(`CREATE FUNCTION schema_children_count() RETURNS Bigint AS $$ SELECT 1::bigint $$ LANGUAGE sql`)
	s.Register("schema_parents", `CREATE TABLE schema_parents ("id" Bigserial PRIMARY KEY)`)
	s.Register("schema_children", `CREATE TABLE schema_children ("parent_id" Bigint REFERENCES missing_table ("id"))`)

	err := s.migrate(context.Background(), false)
	require.ErrorContains(t, err, "[PG] migrate table schema_children: ")
	require.ErrorContains(t, err, `relation "missing_table" does not exist`)
	require.ErrorContains(t, err, `[CREATE TABLE schema_children`)

	// the queries before the failed one are rolled back
	assert.False(t, tableExists(t, "schema_parents"))
	var n int
	require.ErrorContains(t, DB.QueryRow(context.Background(), "SELECT schema_children_count()").Scan(&n), "does not exist")
}

func TestSchemaRegisterFiles(t *testing.T) {
	dropSchemaTables(t)
	s := &schema{}
	require.NoError(t, s.RegisterFiles(&schemaFiles, "testdata/schema"))
	// the files are registered in the file name order, the other files are ignored
	assert.Equal(t, []string{"01_schema_parents", "02_schema_children"}, s.ListTables())

	// a file runs as one query, the semicolons in strings and function bodies are kept
	require.NoError(t, s.migrate(context.Background(), false))
	ctx := context.Background()
	var name string
	require.NoError(t, DB.QueryRow(ctx, `INSERT INTO schema_parents DEFAULT VALUES RETURNING name`).Scan(&name))
	assert.Equal(t, "a;b", name)
	_, err := DB.Exec(ctx, `INSERT INTO schema_children (parent_id) SELECT id FROM schema_parents`)
	require.NoError(t, err)
	var n int
	require.NoError(t, DB.QueryRow(ctx, "SELECT schema_children_count()").Scan(&n))
	assert.Equal(t, 1, n)

	require.Error(t, s.RegisterFiles(&schemaFiles, "testdata/missing"))
}

func TestSchemaRegisterAfterConnect(t *testing.T) {
	t.Cleanup(func() {
		if !DB.Connected() {
			require.NoError(t, Connect(testConfig()))
		}
		_, err := DB.Exec(context.Background(), "DROP TABLE IF EXISTS schema_after_connect")
		require.NoError(t, err)
	})

	// the schema is cleared after the connect migrated it, it can still be registered for the next connect
	require.True(t, DB.Connected())
	Schema.Register("schema_after_connect", `CREATE TABLE IF NOT EXISTS schema_after_connect ("id" Bigserial PRIMARY KEY)`)
	assert.Equal(t, []string{"schema_after_connect"}, Schema.ListTables())

	Close()
	require.NoError(t, Connect(testConfig()))
	n, err := Count("schema_after_connect")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, Schema.ListTables())
}
