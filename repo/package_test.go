package repo

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/require"
)

// testSchema is separate from the pg package tests schema, because packages are tested in parallel.
const testSchema = "orgo_repo"

func TestMain(m *testing.M) {
	beforeAll()
	os.Exit(m.Run())
}

func beforeAll() {
	if err := godotenv.Load("../.env_test"); err != nil {
		log.Fatalln("Error loading env configuration", err)
	}
	// the tables are migrated on connect, before the registered repositories load them into their caches
	pg.Schema.Register("base_models", createBaseModels)
	pg.Schema.Register("order", createOrder)
	if err := pg.Connect(testConfig()); err != nil {
		log.Fatalln("Error opening database connection: ", err)
	}
	// the tables are created again, so every run starts with their current definitions
	sqls := []string{
		`DROP TABLE IF EXISTS base_models;`,
		createBaseModels,
		`DROP TABLE IF EXISTS "order";`,
		createOrder,
	}
	for _, sql := range sqls {
		if _, err := pg.DB.Exec(context.Background(), sql); err != nil {
			log.Fatalln("Error executing SQL: ", err)
		}
	}
}

const (
	createBaseModels = `CREATE TABLE IF NOT EXISTS base_models (
		"id" Bigserial,
		"name" Text,
		"email" Text,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now(),
		PRIMARY KEY ( "id" ) );`
	createOrder = `CREATE TABLE IF NOT EXISTS "order" (
		"id" Bigserial PRIMARY KEY,
		"user" Text,
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
		Schema:       testSchema,
		// the synctest bubbles open all the connections first, see pgtest.Synctest
		MaxConns: 5,
	}
}

type baseModel struct {
	model.Base[model.ID]
	Name  string
	Email string
}

func (m *baseModel) TableName() string {
	return "base_models"
}

type orderModel struct {
	model.Base[model.ID]
	User string // reserved word column
}

func (m *orderModel) TableName() string {
	return testSchema + ".order" // reserved word table, schema-qualified
}

type missingTableModel struct {
	model.CreateOnly[model.ID]
	Name string
}

func (m *missingTableModel) TableName() string {
	return "missing_table"
}

// hook behavior is configured globally, because AfterCreate is called on a new instance mapped from the database
var (
	hookCalls     []string
	hookBeforeErr error
	hookAfterErr  error
)

// hookModel records its hook calls in hookCalls and fails them with hookBeforeErr and hookAfterErr.
type hookModel struct {
	model.Base[model.ID]
	Name  string
	Email string
}

func (m *hookModel) TableName() string {
	return "base_models"
}

func (m *hookModel) BeforeCreate(ctx context.Context, tx pg.Tx) error {
	hookCalls = append(hookCalls, "before:"+m.ID.String())
	m.Email = m.Name + "@example.com"
	return hookBeforeErr
}

func (m *hookModel) AfterCreate(ctx context.Context, tx pg.Tx) error {
	hookCalls = append(hookCalls, "after:"+m.Email)
	return hookAfterErr
}

func (m *hookModel) BeforeUpdate(ctx context.Context, tx pg.Tx) error {
	hookCalls = append(hookCalls, "before-update:"+m.Name)
	m.Email = m.Name + "@updated.example.com"
	return hookBeforeErr
}

func (m *hookModel) AfterUpdate(ctx context.Context, tx pg.Tx) error {
	hookCalls = append(hookCalls, "after-update:"+m.Email)
	return hookAfterErr
}

func (m *hookModel) BeforeDelete(ctx context.Context, tx pg.Tx) error {
	hookCalls = append(hookCalls, "before-delete:"+m.ID.String()+":"+m.Name)
	return hookBeforeErr
}

func (m *hookModel) AfterDelete(ctx context.Context, tx pg.Tx) error {
	hookCalls = append(hookCalls, "after-delete:"+m.ID.String()+":"+m.Name)
	return hookAfterErr
}

func setupHookTest(t *testing.T) {
	t.Helper()
	require.NoError(t, pg.ClearTables("base_models"))
	hookCalls, hookBeforeErr, hookAfterErr = nil, nil, nil
	t.Cleanup(func() { hookCalls, hookBeforeErr, hookAfterErr = nil, nil, nil })
}

type ctxKey struct{}

// ctxHookCalls records the ctx value seen by each hook of ctxModel
var ctxHookCalls []string

type ctxModel struct {
	model.Base[model.ID]
	Name string
}

func (m *ctxModel) TableName() string {
	return "base_models"
}

func recordCtx(ctx context.Context, hook string) {
	v, _ := ctx.Value(ctxKey{}).(string)
	ctxHookCalls = append(ctxHookCalls, hook+":"+v)
}

func (m *ctxModel) BeforeCreate(ctx context.Context, tx pg.Tx) error {
	recordCtx(ctx, "before-create")
	return nil
}

func (m *ctxModel) AfterCreate(ctx context.Context, tx pg.Tx) error {
	recordCtx(ctx, "after-create")
	return nil
}

func (m *ctxModel) BeforeUpdate(ctx context.Context, tx pg.Tx) error {
	recordCtx(ctx, "before-update")
	return nil
}

func (m *ctxModel) AfterUpdate(ctx context.Context, tx pg.Tx) error {
	recordCtx(ctx, "after-update")
	return nil
}

func (m *ctxModel) BeforeDelete(ctx context.Context, tx pg.Tx) error {
	recordCtx(ctx, "before-delete")
	return nil
}

func (m *ctxModel) AfterDelete(ctx context.Context, tx pg.Tx) error {
	recordCtx(ctx, "after-delete")
	return nil
}

func setupCtxTest(t *testing.T) {
	t.Helper()
	require.NoError(t, pg.ClearTables("base_models"))
	ctxHookCalls = nil
	t.Cleanup(func() { ctxHookCalls = nil })
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	n, err := pg.Count(table)
	require.NoError(t, err)
	return n
}
