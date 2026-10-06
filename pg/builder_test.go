package pg

import (
	"context"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type BaseModelData struct {
	Name  string         `json:"name,omitempty"`
	Email string         `json:"email,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type baseModel struct {
	model.Base[model.ID]
	Name  string
	Email string
	Data  BaseModelData
}

func (u *baseModel) TableName() string {
	return "base_models"
}

type createOnlyModel struct {
	model.CreateOnly[model.ID]
	Name  string
	Email string
}

func (u *createOnlyModel) TableName() string {
	return "create_only_models"
}

func TestBuildUpdate(t *testing.T) {
	base := &baseModel{
		ID:    1,
		Name:  "John Doe",
		Email: "john.doe@example.com",
		Data:  BaseModelData{Name: "John Doe", Email: "john.doe@example.com", Data: map[string]any{"name": "John Doe", "email": "john.doe@example.com"}},
	}

	query, err := BuildUpdate(base)
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "base_models" SET "updated" = NOW(), "name" = $1, "email" = $2, "data" = $3 WHERE "id" = $4 RETURNING *`, query.SQL)
	assert.Equal(t, []any{base.Name, base.Email, base.Data, model.ID(1)}, query.Args)

	createOnly := &createOnlyModel{
		ID:    1,
		Name:  "John Doe",
		Email: "john.doe@example.com",
	}

	_, err = BuildUpdate(createOnly)
	require.EqualError(t, err, "[PG] model createOnlyModel is create only")
}

func TestBuildUpdateWithFields(t *testing.T) {
	base := &baseModel{
		ID:    1,
		Name:  "John Doe",
		Email: "john.doe@example.com",
		Data:  BaseModelData{Name: "John Doe", Email: "john.doe@example.com", Data: map[string]any{"name": "John Doe", "email": "john.doe@example.com"}},
	}

	query, err := BuildUpdate(base, "name")
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "base_models" SET "updated" = NOW(), "name" = $1 WHERE "id" = $2 RETURNING *`, query.SQL)
	assert.Equal(t, []any{"John Doe", model.ID(1)}, query.Args)

	query, err = BuildUpdate(base, "email")
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "base_models" SET "updated" = NOW(), "email" = $1 WHERE "id" = $2 RETURNING *`, query.SQL)
	assert.Equal(t, []any{"john.doe@example.com", model.ID(1)}, query.Args)

	query, err = BuildUpdate(base, "data")
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "base_models" SET "updated" = NOW(), "data" = $1 WHERE "id" = $2 RETURNING *`, query.SQL)
	assert.Equal(t, []any{base.Data, model.ID(1)}, query.Args)
}

func TestBuildInsertBaseModel(t *testing.T) {
	user := &baseModel{
		Name:  "John Doe",
		Email: "john.doe@example.com",
		Data:  BaseModelData{Name: "John Doe", Email: "john.doe@example.com", Data: map[string]any{"name": "John Doe", "email": "john.doe@example.com"}},
	}

	query, err := BuildInsert(user)
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "base_models" ("created", "updated", "name", "email", "data") VALUES (NOW(), NOW(), $1, $2, $3) RETURNING *`, query.SQL)
	assert.Equal(t, []any{"John Doe", "john.doe@example.com", user.Data}, query.Args)
}

func TestBuildInsertCreateOnlyModel(t *testing.T) {
	user := &createOnlyModel{
		Name:  "John Doe",
		Email: "john.doe@example.com",
	}

	query, err := BuildInsert(user)
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "create_only_models" ("created", "name", "email") VALUES (NOW(), $1, $2) RETURNING *`, query.SQL)
	assert.Equal(t, []any{"John Doe", "john.doe@example.com"}, query.Args)
}

type BuilderEmbedded struct {
	Name string
}

type builderEmbeddedPtrModel struct {
	model.CreateOnly[model.ID]
	*BuilderEmbedded
	Email string
}

func (m *builderEmbeddedPtrModel) TableName() string {
	return "embedded_models"
}

func TestBuildInsertNilEmbeddedPointer(t *testing.T) {
	// the fields behind a nil embedded pointer are zero, they are left to the column defaults
	query, err := BuildInsert(&builderEmbeddedPtrModel{Email: "john.doe@example.com"})
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "embedded_models" ("created", "email") VALUES (NOW(), $1) RETURNING *`, query.SQL)
	assert.Equal(t, []any{"john.doe@example.com"}, query.Args)

	query, err = BuildInsert(&builderEmbeddedPtrModel{BuilderEmbedded: &BuilderEmbedded{Name: "John"}})
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "embedded_models" ("created", "name") VALUES (NOW(), $1) RETURNING *`, query.SQL)
	assert.Equal(t, []any{"John"}, query.Args)
}

type builderValuesModel struct {
	model.Base[model.ID]
	Order  int // reserved word
	Active bool
	Note   *string
	Tags   []string
}

func (m *builderValuesModel) TableName() string {
	return "values_models"
}

func TestBuildZeroValues(t *testing.T) {
	// zero values are not inserted, the column defaults apply to them
	query, err := BuildInsert(&builderValuesModel{})
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "values_models" ("created", "updated") VALUES (NOW(), NOW()) RETURNING *`, query.SQL)
	assert.Empty(t, query.Args)

	// also when the fields are given, a non-nil pointer to a zero value and an empty slice are not zero
	note := ""
	query, err = BuildInsert(&builderValuesModel{Note: &note, Tags: []string{}}, "order", "note", "tags")
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "values_models" ("created", "updated", "note", "tags") VALUES (NOW(), NOW(), $1, $2) RETURNING *`, query.SQL)
	assert.Equal(t, []any{&note, []string{}}, query.Args)

	// a model without timestamps and only zero values inserts the defaults
	query, err = BuildInsert(&plainModel{})
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "plain_models" DEFAULT VALUES RETURNING *`, query.SQL)
	assert.Empty(t, query.Args)

	// updates write zero values, a field can be cleared

	query, err = BuildUpdate(&builderValuesModel{Base: model.Base[model.ID]{ID: 3}, Note: &note}, "active", "note")
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "values_models" SET "updated" = NOW(), "active" = $1, "note" = $2 WHERE "id" = $3 RETURNING *`, query.SQL)
	assert.Equal(t, []any{false, &note, model.ID(3)}, query.Args)
}

func TestBuildInsertWithFields(t *testing.T) {
	query, err := BuildInsert(&baseModel{Name: "John Doe", Email: "john.doe@example.com"}, "email")
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "base_models" ("created", "updated", "email") VALUES (NOW(), NOW(), $1) RETURNING *`, query.SQL)
	assert.Equal(t, []any{"john.doe@example.com"}, query.Args)
}

func TestBuildInvalidFields(t *testing.T) {
	base := &baseModel{Base: model.Base[model.ID]{ID: 1}}
	for _, field := range []string{"missing", "id", "created", "updated", "Name"} {
		_, err := BuildUpdate(base, field)
		assert.EqualError(t, err, `[PG] model baseModel has no writable field "`+field+`"`)
		_, err = BuildInsert(base, field)
		assert.EqualError(t, err, `[PG] model baseModel has no writable field "`+field+`"`)
	}
}

func TestBuildUpdateInvalidID(t *testing.T) {
	_, err := BuildUpdate(&baseModel{Name: "John Doe"})
	assert.EqualError(t, err, `[PG] model baseModel has invalid id "0"`)
}

func TestBuildNilModel(t *testing.T) {
	_, err := BuildInsert[*baseModel](nil)
	assert.EqualError(t, err, "[PG] model *pg.baseModel is nil")
	_, err = BuildUpdate[*baseModel](nil)
	assert.EqualError(t, err, "[PG] model *pg.baseModel is nil")
}

func TestBuildQueriesRunOnDatabase(t *testing.T) {
	require.NoError(t, ClearTables("base_models"))
	ctx := context.Background()

	created, err := CreateModel(&baseModel{Name: "John Doe", Email: "john.doe@example.com"})
	require.NoError(t, err)

	// clear the name: must be stored as an empty string, not NULL
	created.Name = ""
	query, err := BuildUpdate(created, "name")
	require.NoError(t, err)
	res, err := MapRows[*baseModel](queryRows(t, query.SQL, query.Args...))
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "", res[0].Name)
	assert.Equal(t, "john.doe@example.com", res[0].Email)
	assert.True(t, res[0].Updated.After(created.Updated) || res[0].Updated.Equal(created.Updated))

	var nameIsNull bool
	require.NoError(t, DB.QueryRow(ctx, "SELECT name IS NULL FROM base_models WHERE id = $1", created.ID).Scan(&nameIsNull))
	assert.False(t, nameIsNull)
}

func TestQuoteTable(t *testing.T) {
	assert.Equal(t, `"users"`, QuoteTable("users"))
	assert.Equal(t, `"order"`, QuoteTable("order"))
	assert.Equal(t, `"public"."users"`, QuoteTable("public.users"))
	assert.Equal(t, `"Users"`, QuoteTable("Users"))
	// already quoted names are kept as they are
	assert.Equal(t, `"public"."users"`, QuoteTable(`"public"."users"`))
	assert.Equal(t, `public."order"`, QuoteTable(`public."order"`))
}

type builderOrderModel struct {
	model.Base[model.ID]
	User string // reserved word column
}

func (m *builderOrderModel) TableName() string {
	return "orgo.order" // reserved word table, schema-qualified
}

func TestReservedTableNameOnDatabase(t *testing.T) {
	ctx := context.Background()
	_, err := DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS orgo."order" (
		"id" Bigserial PRIMARY KEY,
		"user" Text,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now())`)
	require.NoError(t, err)
	require.NoError(t, ClearTables("orgo.order"))

	created, err := CreateModel(&builderOrderModel{User: "john"})
	require.NoError(t, err)
	require.True(t, created.ID.Valid())

	found, err := Query(&builderOrderModel{}).Where("id = ?", created.ID).First()
	require.NoError(t, err)
	assert.Equal(t, "john", found.User)

	found.User = "jane"
	query, err := BuildUpdate(found, "user")
	require.NoError(t, err)
	res, err := MapRows[*builderOrderModel](queryRows(t, query.SQL, query.Args...))
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "jane", res[0].User)

	n, err := Count("orgo.order")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = CountWhereWithContext(ctx, "orgo.order", `"user" = ?`, "jane")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = CountWhere("orgo.order", `"user" = ?`, "john")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	require.NoError(t, ClearTables("orgo.order"))
	n, err = CountWithContext(ctx, "orgo.order")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestBuildDelete(t *testing.T) {
	query, err := BuildDelete(&baseModel{Base: model.Base[model.ID]{ID: 5}})
	require.NoError(t, err)
	assert.Equal(t, `DELETE FROM "base_models" WHERE "id" = $1 RETURNING *`, query.SQL)
	assert.Equal(t, []any{model.ID(5)}, query.Args)

	// create only models can be deleted
	query, err = BuildDelete(&createOnlyModel{CreateOnly: model.CreateOnly[model.ID]{ID: 5}})
	require.NoError(t, err)
	assert.Equal(t, `DELETE FROM "create_only_models" WHERE "id" = $1 RETURNING *`, query.SQL)

	_, err = BuildDelete(&baseModel{})
	assert.EqualError(t, err, `[PG] model baseModel has invalid id "0"`)
	_, err = BuildDelete[*baseModel](nil)
	assert.EqualError(t, err, "[PG] model *pg.baseModel is nil")
}

type builderUUIDModel struct {
	model.CreateOnly[model.UUID]
	Name string
}

func (m *builderUUIDModel) TableName() string {
	return "uuid_models"
}

func TestBuildInsertWithID(t *testing.T) {
	// an ID set by the application is inserted
	query, err := BuildInsert(&builderUUIDModel{CreateOnly: model.CreateOnly[model.UUID]{ID: "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"}, Name: "john"})
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "uuid_models" ("id", "created", "name") VALUES ($1, NOW(), $2) RETURNING *`, query.SQL)
	assert.Equal(t, []any{model.UUID("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"), "john"}, query.Args)

	// the id is inserted also when fields are given
	query, err = BuildInsert(&baseModel{Base: model.Base[model.ID]{ID: 7}, Name: "john"}, "name")
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "base_models" ("id", "created", "updated", "name") VALUES ($1, NOW(), NOW(), $2) RETURNING *`, query.SQL)
	assert.Equal(t, []any{model.ID(7), "john"}, query.Args)

	// an invalid ID is left to the database
	query, err = BuildInsert(&builderUUIDModel{Name: "john"})
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "uuid_models" ("created", "name") VALUES (NOW(), $1) RETURNING *`, query.SQL)
}

func TestCreateModelWithID(t *testing.T) {
	ctx := context.Background()
	_, err := DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS uuid_models (
		"id" Uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		"name" Text,
		"created" Timestamptz DEFAULT now())`)
	require.NoError(t, err)
	require.NoError(t, ClearTables("uuid_models"))

	id := model.UUID("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	created, err := CreateModel(&builderUUIDModel{CreateOnly: model.CreateOnly[model.UUID]{ID: id}, Name: "john"})
	require.NoError(t, err)
	assert.Equal(t, id, created.ID)

	generated, err := CreateModel(&builderUUIDModel{Name: "jane"})
	require.NoError(t, err)
	assert.True(t, generated.ID.Valid())
	assert.NotEqual(t, id, generated.ID)

	found, err := Query(&builderUUIDModel{}).Where("id = ?", id).First()
	require.NoError(t, err)
	assert.Equal(t, "john", found.Name)
}
