package actions

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/ordaen/orgo/pg"
)

func TestMain(m *testing.M) {
	if err := godotenv.Load("../.env_test"); err != nil {
		log.Fatalln("Error loading env configuration", err)
	}
	RegisterSchema()
	if err := pg.Connect(pg.Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo_actions",
	}); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the table is created again, so every run starts with its current definition
	for _, sql := range []string{`DROP TABLE IF EXISTS action_logs;`, SQLSchema} {
		if _, err := pg.DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
	os.Exit(m.Run())
}
