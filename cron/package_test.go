package cron

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/settings"
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
	pg.Schema.Register("settings", settings.SQLSchema)
	pg.Schema.Register("cron_records", SQLSchemaRecords)
	pg.Schema.Register("cron_logs", SQLSchemaLogs)
	pg.Schema.Register("cron_log_messages", SQLSchemaLogMessages)
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the tables are created again, so every run starts with their current definitions
	sqls := []string{
		`DROP TABLE IF EXISTS settings;`, settings.SQLSchema,
		`DROP TABLE IF EXISTS cron_records;`, SQLSchemaRecords,
		`DROP TABLE IF EXISTS cron_logs;`, SQLSchemaLogs,
		`DROP TABLE IF EXISTS cron_log_messages;`, SQLSchemaLogMessages,
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
		Schema:       "orgo_cron",
		// the synctest bubbles open all the connections first, see pgtest.Synctest
		MaxConns: 5,
	}
}
