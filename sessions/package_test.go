package sessions

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	beforeAll()
	retCode := m.Run()
	afterAll()
	os.Exit(retCode)
}

func beforeAll() {
	if err := godotenv.Load("../.env_test"); err != nil {
		log.Fatalln("Error loading env configuration", err)
	}
	pg.Schema.Register("sessions", SQLSchemaSessions)
	pg.Schema.Register("blocked_ips", SQLSchemaBlockedIPs)
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the tables are created again, so every run starts with their current definitions
	sqls := []string{
		`DROP TABLE IF EXISTS sessions;`,
		SQLSchemaSessions,
		`DROP TABLE IF EXISTS blocked_ips;`,
		SQLSchemaBlockedIPs,
	}
	for _, sql := range sqls {
		if _, err := pg.DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
}

func afterAll() {}

// testConfig returns the test database configuration loaded from .env_test.
func testConfig() pg.Config {
	return pg.Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo_sessions",
	}
}

// clearTables deletes the records of the tables and reloads the caches of the repositories,
// pg.ClearTables does not go through them.
func clearTables(t *testing.T, tables ...string) {
	t.Helper()
	require.NoError(t, pg.ClearTables(tables...))
	require.NoError(t, Sessions.Setup())
	require.NoError(t, BlockedIPs.Setup())
}
