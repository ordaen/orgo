package gql

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/pg"
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
	// the cached repositories of sessions are loaded on connect
	RegisterSchema()
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the tables are created again, so every run starts with their current definitions
	sqls := []string{
		`DROP TABLE IF EXISTS logs;`, logger.SQLSchema,
		`DROP TABLE IF EXISTS items;`, createItems,
	}
	for _, sql := range sqls {
		if _, err := pg.DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
}

func afterAll() {}

const createItems = `CREATE TABLE items (
	"id" Bigserial PRIMARY KEY,
	"name" Text,
	"owner_id" Bigint,
	"active" Boolean DEFAULT FALSE,
	"created" Timestamptz DEFAULT now(),
	"updated" Timestamptz DEFAULT now());`

// testConfig returns the test database configuration loaded from .env_test.
func testConfig() pg.Config {
	return pg.Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo_gql",
	}
}
