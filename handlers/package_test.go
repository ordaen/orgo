package handlers

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/sessions"
)

func TestMain(m *testing.M) {
	if err := godotenv.Load("../.env_test"); err != nil {
		log.Fatalln("Error loading env configuration", err)
	}
	// the cached repositories of sessions are loaded on connect
	sessions.RegisterSchema()
	if err := pg.Connect(pg.Config{
		DatabaseName: os.Getenv("DATABASE_NAME"),
		Username:     os.Getenv("DATABASE_USER"),
		Password:     os.Getenv("DATABASE_PASSWORD"),
		Host:         os.Getenv("DATABASE_HOST"),
		Schema:       "orgo_handlers",
	}); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the tables are created again, so every run starts with their current definitions
	for _, sql := range []string{`DROP TABLE IF EXISTS items;`, createItems, `DROP TABLE IF EXISTS logs;`, logger.SQLSchema} {
		if _, err := pg.DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
	os.Exit(m.Run())
}

const createItems = `CREATE TABLE items (
	"id" Bigserial PRIMARY KEY,
	"name" Text,
	"owner_id" Bigint,
	"active" Boolean DEFAULT FALSE,
	"ip" Cidr,
	"created" Timestamptz DEFAULT now(),
	"updated" Timestamptz DEFAULT now());`

type item struct {
	model.Base[model.ID]
	Name    string   `json:"name"`
	OwnerID model.ID `json:"owner_id"`
	Active  bool     `json:"active"`
	IP      string   `json:"ip"`
}

func (*item) TableName() string { return "items" }
func (*item) SortBy() string    { return "name" }

// ipItem is an item with the IP search and sort of warden.
type ipItem struct{ item }

func (*ipItem) SortBy() string { return "ip" }

func (*ipItem) CustomQueries(key, val string) (string, []any, bool) {
	if key != "ip" {
		return "", nil, false
	}
	return `"ip" >>= ?::cidr OR "ip" << ?::cidr`, []any{val, val}, true
}

func (*ipItem) CustomSort(sort []SortColumn) (string, bool) {
	if len(sort) == 1 && sort[0].Column == "ip" {
		return sort[0].Expr() + `, masklen("ip")`, true
	}
	return "", false
}
