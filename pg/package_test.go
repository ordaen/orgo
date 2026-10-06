package pg

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
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
	if err := Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	sqls := []string{
		`DROP TABLE IF EXISTS base_models;`,
		`CREATE TABLE IF NOT EXISTS base_models (
			"id" Bigserial,
			"name" Text,
			"email" Text,
			"data" JSONB,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now(),
			PRIMARY KEY ( "id" ) );`,
		`DROP TABLE IF EXISTS create_only_models;`,
		`CREATE TABLE IF NOT EXISTS create_only_models (
			"id" Bigserial,
			"name" Text,
			"email" Text,
			"created" Timestamptz DEFAULT now(),
			PRIMARY KEY ( "id" ) );`,
	}
	for _, sql := range sqls {
		if _, err := DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
}

func afterAll() {}

// testConfig returns the test database configuration loaded from .env_test.
func testConfig() Config {
	return Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo",
	}
}
