package statuser

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
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
	pg.Schema.Register("status_records", SQLSchema)
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the tables are created again, so every run starts with their current definitions
	sqls := []string{
		`DROP TABLE IF EXISTS status_records;`,
		SQLSchema,
		`DROP TABLE IF EXISTS orders;`,
		createOrders,
		`DROP TABLE IF EXISTS documents;`,
		createDocuments,
	}
	for _, sql := range sqls {
		if _, err := pg.DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
}

func afterAll() {}

const (
	createOrders = `CREATE TABLE orders (
		"id" Bigserial PRIMARY KEY,
		"status" Text NOT NULL,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now());`
	createDocuments = `CREATE TABLE documents (
		"id" UUid PRIMARY KEY,
		"status" Text NOT NULL,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now());`
)

// testConfig returns the test database configuration loaded from .env_test.
func testConfig() pg.Config {
	return pg.Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo_statuser",
		// the synctest bubbles open all the connections first, see pgtest.Synctest
		MaxConns: 5,
	}
}
