package logger

import (
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
	pg.Schema.Register("logger", SQLSchema)
	pg.Schema.Register("test_models", createTestModels)
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
}

func afterAll() {}

const createTestModels = `CREATE TABLE IF NOT EXISTS test_models (
	"id" Bigserial,
	"name" Text,
	"created" Timestamptz DEFAULT now(),
	"updated" Timestamptz DEFAULT now(),
	PRIMARY KEY ( "id" ) );`

// testConfig returns the test database configuration loaded from .env_test.
func testConfig() pg.Config {
	return pg.Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo_logger",
	}
}
