package files

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
	RegisterSchema()
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
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
		Schema:       "orgo_files",
	}
}
