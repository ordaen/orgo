package pg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainsDropTable(t *testing.T) {
	tests := []struct {
		sql  string
		want bool
	}{
		{"DROP TABLE users", true},
		{"drop table if exists users", true},
		{"DROP\n\tTABLE users", true},
		{"DROP  /* comment */ TABLE users", true},
		{"DROP -- comment\nTABLE users", true},
		{"DROP FOREIGN TABLE users", true},
		{"CREATE TABLE a (); DROP TABLE b", true},

		{"CREATE TABLE users ()", false},
		{"ALTER TABLE users DROP COLUMN name", false},
		{"DROP INDEX users_name", false},
		{"DROP VIEW users_view", false},
		{"CREATE TABLE droptable (); CREATE TABLE drop_table ()", false},
		{"DROP; TABLE", false},

		// comments are skipped, also nested ones
		{"-- we never drop table here\nCREATE TABLE users ()", false},
		{"/* drop table users */ CREATE TABLE users ()", false},
		{"/* outer /* inner */ drop table users */ CREATE TABLE users ()", false},
		// quoted identifiers are not keywords
		{`CREATE TABLE "drop table" ()`, false},
		{`CREATE TABLE t ("drop" Text, "table" Text)`, false},

		// comment markers in strings do not start comments
		{"SELECT '--'; DROP TABLE users", true},
		{"SELECT '/*'; DROP TABLE users", true},
		{"INSERT INTO t VALUES ('it''s -- fine')", false},
		{`SELECT E'a\'b'; DROP TABLE users`, true},
		{`SELECT 'a\'; DROP TABLE users`, true},
		{"SELECT $1; DROP TABLE users", true},
		{"SELECT a$1 FROM t; DROP TABLE users", true},

		// DO blocks and EXECUTE run their strings, so they are scanned as SQL
		{"DO $$ BEGIN DROP TABLE users; END $$", true},
		{"DO $body$ BEGIN EXECUTE 'DROP TABLE users'; END $body$", true},
		{"DO $a$ BEGIN EXECUTE $b$ DROP TABLE users $b$; END $a$", true},
		{"CREATE FUNCTION f() RETURNS void AS $$ SELECT 1; $$ LANGUAGE sql", false},
		// a data string mentioning it is reported too, the guard cannot tell data from code
		{"INSERT INTO notes VALUES ('drop table')", true},

		// unterminated comments, strings and identifiers run to the end
		{"CREATE TABLE t (); /* DROP TABLE t", false},
		{"SELECT 'DROP TABLE t", true},
		{`SELECT "DROP TABLE t`, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, containsDropTable(tt.sql), tt.sql)
	}
}

func TestSchemaDropTableGuard(t *testing.T) {
	dropSchemaTables(t)
	ctx := context.Background()

	// whitespace and comments between the keywords do not hide the drop
	s := &schema{}
	s.Register("tt1", "CREATE TABLE IF NOT EXISTS tt1 ()", "DROP\n/* x */ TABLE tt1")
	require.EqualError(t, s.migrate(ctx, false), "[PG] migrate table tt1: DROP TABLE is not allowed: [DROP\n/* x */ TABLE tt1]")

	// the functions are checked too, a DO block runs when it is registered
	s = &schema{}
	s.RegisterFunction("DO $$ BEGIN DROP TABLE IF EXISTS tt1; END $$")
	require.ErrorContains(t, s.migrate(ctx, false), "[PG] migrate function 1: DROP TABLE is not allowed")

	// a comment mentioning it is allowed
	s = &schema{}
	s.Register("tt1", "-- the table is never dropped, see DROP TABLE policy\nCREATE TABLE IF NOT EXISTS tt1 ()")
	require.NoError(t, s.migrate(ctx, false))
	assert.True(t, tableExists(t, "tt1"))
}

// FuzzContainsDropTable checks that the scanner returns on any input. Run it with -parallel 1, the fuzz workers
// run TestMain, which recreates the test tables:
//
//	go test -run '^$' -fuzz FuzzContainsDropTable -parallel 1 .
func FuzzContainsDropTable(f *testing.F) {
	for _, s := range []string{"DROP TABLE t", "$$", "$a$ x $a", "E'\\", "'", `"`, "/* /*", "--", "$1$", "drop foreign"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		containsDropTable(sql)
	})
}
