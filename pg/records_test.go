package pg

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateModel(t *testing.T) {
	require.NoError(t, ClearTables("base_models", "create_only_models"))
	model := &baseModel{
		Name:  "John Doe",
		Email: "john.doe@example.com",
		Data:  BaseModelData{Name: "John Doe", Email: "john.doe@example.com", Data: map[string]any{"name": "John Doe", "email": "john.doe@example.com"}},
	}
	createdModel, err := CreateModel(model)
	require.NoError(t, err)
	require.Equal(t, model.Name, createdModel.Name)
	require.Equal(t, model.Email, createdModel.Email)
	require.Equal(t, model.Data, createdModel.Data)
	require.NotZero(t, createdModel.ID)
	require.NotZero(t, createdModel.Created)
	require.NotZero(t, createdModel.Updated)
	// fmt.Printf("DEBUG: %#v\n\n", createdModel)
	// require.False(t, true)
}

// failingScanner can be written to the database, but fails when it is read back.
// set makes it a non-zero value, zero values are not inserted.
type failingScanner struct{ set bool }

func (failingScanner) Value() (driver.Value, error) { return "value", nil }

func (*failingScanner) Scan(any) error { return errors.New("scan failed") }

type rollbackModel struct {
	model.CreateOnly[model.ID]
	Code failingScanner
}

func (m *rollbackModel) TableName() string {
	return "rollback_models"
}

func TestCreateModelRollsBackOnMapError(t *testing.T) {
	ctx := context.Background()
	_, err := DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS rollback_models (
		"id" Bigserial PRIMARY KEY,
		"code" Text,
		"created" Timestamptz DEFAULT now())`)
	require.NoError(t, err)
	require.NoError(t, ClearTables("rollback_models"))

	_, err = CreateModel(&rollbackModel{Code: failingScanner{set: true}})
	require.ErrorContains(t, err, "scan failed")

	n, err := CountWithContext(ctx, "rollback_models")
	require.NoError(t, err)
	assert.Zero(t, n, "insert must be rolled back when mapping fails")
}

func TestCreateModelErrors(t *testing.T) {
	// builder errors are returned as they are, without a second prefix
	_, err := CreateModel[*baseModel](nil)
	require.EqualError(t, err, "[PG] model *pg.baseModel is nil")

	_, err = CreateModel(&missingTableModel{})
	require.ErrorContains(t, err, "[PG] insert into missing_table: ")
	assert.Equal(t, 1, strings.Count(err.Error(), "[PG]"))
}

type missingTableModel struct {
	model.CreateOnly[model.ID]
	Name string
}

func (m *missingTableModel) TableName() string {
	return "missing_table"
}

func TestInsert(t *testing.T) {
	setupHookTest(t)

	inserted, err := Insert(&hookModel{Name: "john"})
	require.NoError(t, err)
	require.True(t, inserted.ID.Valid())
	assert.Equal(t, "john", inserted.Name)
	assert.Empty(t, inserted.Email, "BeforeCreate is not called")
	assert.NotZero(t, inserted.Created)
	assert.Empty(t, hookCalls)
	assert.Zero(t, countRows(t, "hook_logs"), "AfterCreate is not called")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = InsertWithContext(ctx, &hookModel{Name: "jane"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, countRows(t, "base_models"))
}

func TestInsertErrors(t *testing.T) {
	_, err := Insert[*baseModel](nil)
	require.EqualError(t, err, "[PG] model *pg.baseModel is nil")

	_, err = Insert(&missingTableModel{})
	require.ErrorContains(t, err, "[PG] insert into missing_table: ")
	assert.Equal(t, 1, strings.Count(err.Error(), "[PG]"))

	// the DBErrorHandler is called
	setupDBErrorTest(t)
	_, err = Insert(&blockedIP{IP: "1.1.1.1"})
	require.NoError(t, err)
	_, err = Insert(&blockedIP{IP: "1.1.1.1"})
	assert.EqualError(t, err, "ip: 1.1.1.1 already exists")
	assert.Equal(t, []string{"create:23505"}, handledOps)
}

func TestCountErrors(t *testing.T) {
	_, err := Count("missing_table")
	require.ErrorContains(t, err, "[PG] count missing_table: ")

	_, err = CountWhere("base_models", "missing_column = ?", 1)
	require.ErrorContains(t, err, "[PG] count base_models: ")
}

func TestCountWithContext(t *testing.T) {
	n, err := CountWithContext(context.Background(), "base_models")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = CountWithContext(ctx, "base_models")
	require.ErrorIs(t, err, context.Canceled)

	_, err = CountWhereWithContext(ctx, "base_models", "id > ?", 0)
	require.ErrorIs(t, err, context.Canceled)
}

func TestExec(t *testing.T) {
	require.NoError(t, ClearTables("base_models"))
	_, err := CreateModel(&baseModel{Name: "john"})
	require.NoError(t, err)
	_, err = CreateModel(&baseModel{Name: "jane"})
	require.NoError(t, err)

	n, err := Exec(`UPDATE base_models SET email = $1 WHERE name = $2`, "john@example.com", "john")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = Exec(`DELETE FROM base_models`)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	_, err = Exec(`DELETE FROM missing_table`)
	require.ErrorContains(t, err, "[PG] exec: ")
	var pgErr *PgError
	require.ErrorAs(t, err, &pgErr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ExecWithContext(ctx, `DELETE FROM base_models`)
	require.ErrorIs(t, err, context.Canceled)
}

func TestClearTablesError(t *testing.T) {
	err := ClearTables("base_models", "missing_table")
	require.ErrorContains(t, err, "[PG] clear missing_table: ")
}

func TestUpdateModel(t *testing.T) {
	require.NoError(t, ClearTables("base_models"))
	created, err := CreateModel(&baseModel{Name: "John Doe", Email: "john.doe@example.com"})
	require.NoError(t, err)

	created.Name = "Jane Doe"
	created.Data = BaseModelData{Name: "Jane"}
	updated, err := UpdateModel(created)
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "Jane Doe", updated.Name)
	assert.Equal(t, "john.doe@example.com", updated.Email)
	assert.Equal(t, BaseModelData{Name: "Jane"}, updated.Data)
	assert.True(t, updated.Created.Equal(created.Created), "created must not change")
	assert.True(t, updated.Updated.After(created.Updated), "updated must be set to NOW()")

	// only the given fields are written
	updated.Name = "ignored"
	updated.Email = "jane.doe@example.com"
	updated2, err := UpdateModel(updated, "email")
	require.NoError(t, err)
	assert.Equal(t, "Jane Doe", updated2.Name)
	assert.Equal(t, "jane.doe@example.com", updated2.Email)

	found, err := Query(&baseModel{}).Where("id = ?", created.ID).First()
	require.NoError(t, err)
	assert.Equal(t, updated2, found)
}

func TestUpdateModelErrors(t *testing.T) {
	require.NoError(t, ClearTables("base_models", "create_only_models"))

	missing := &baseModel{Base: model.Base[model.ID]{ID: 999999}, Name: "John Doe"}
	_, err := UpdateModel(missing)
	require.ErrorIs(t, err, ErrNoRowsAffected)

	_, err = UpdateModel(&createOnlyModel{CreateOnly: model.CreateOnly[model.ID]{ID: 1}})
	require.EqualError(t, err, "[PG] model createOnlyModel is create only")

	_, err = UpdateModel(&missingTableModelUpdatable{Base: model.Base[model.ID]{ID: 1}})
	require.ErrorContains(t, err, "[PG] update missing_table: ")
}

type missingTableModelUpdatable struct {
	model.Base[model.ID]
	Name string
}

func (m *missingTableModelUpdatable) TableName() string {
	return "missing_table"
}

func TestDeleteModel(t *testing.T) {
	require.NoError(t, ClearTables("base_models", "create_only_models"))
	ctx := context.Background()
	created, err := CreateModel(&baseModel{Name: "John Doe", Email: "john.doe@example.com"})
	require.NoError(t, err)
	other, err := CreateModel(&baseModel{Name: "Jane Doe"})
	require.NoError(t, err)

	// the deleted record is returned as it was stored, not the given model
	deleted, err := DeleteModel(&baseModel{Base: model.Base[model.ID]{ID: created.ID}})
	require.NoError(t, err)
	assert.Equal(t, created, deleted)

	_, err = Query(&baseModel{}).Where("id = ?", created.ID).First()
	require.ErrorIs(t, err, ErrRecordNotFound)
	n, err := CountWithContext(ctx, "base_models")
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the model record must be deleted")
	_, err = Query(&baseModel{}).Where("id = ?", other.ID).First()
	require.NoError(t, err)

	// deleting again finds no record
	_, err = DeleteModel(created)
	require.ErrorIs(t, err, ErrNoRowsAffected)

	// create only models can be deleted
	createOnly, err := CreateModel(&createOnlyModel{Name: "John Doe"})
	require.NoError(t, err)
	_, err = DeleteModel(createOnly)
	require.NoError(t, err)
	n, err = CountWithContext(ctx, "create_only_models")
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestDeleteModelErrors(t *testing.T) {
	_, err := DeleteModel(&baseModel{})
	require.EqualError(t, err, `[PG] model baseModel has invalid id "0"`)

	_, err = DeleteModel(&missingTableModel{CreateOnly: model.CreateOnly[model.ID]{ID: 1}})
	require.ErrorContains(t, err, "[PG] delete from missing_table: ")
}

// plainModel is a model without timestamps, not embedding a model base struct.
type plainModel struct {
	ID   model.ID
	Name string
}

func (m *plainModel) GetID() model.ModelID { return m.ID }
func (m *plainModel) TableName() string    { return "plain_models" }

func TestUpdatePlainModel(t *testing.T) {
	ctx := context.Background()
	_, err := DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS plain_models ("id" Bigserial PRIMARY KEY, "name" Text)`)
	require.NoError(t, err)
	require.NoError(t, ClearTables("plain_models"))

	created, err := CreateModel(&plainModel{Name: "john"})
	require.NoError(t, err)
	created.Name = "jane"
	updated, err := UpdateModel(created)
	require.NoError(t, err)
	assert.Equal(t, &plainModel{ID: created.ID, Name: "jane"}, updated)

	query, err := BuildUpdate(updated)
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "plain_models" SET "name" = $1 WHERE "id" = $2 RETURNING *`, query.SQL)
}

func TestNilModelInterface(t *testing.T) {
	// a nil interface returns an error like a nil pointer, it does not panic
	var m Model
	_, err := CreateModel(m)
	require.EqualError(t, err, "[PG] model is nil")
	_, err = UpdateModel(m)
	require.EqualError(t, err, "[PG] model is nil")
	_, err = DeleteModel(m)
	require.EqualError(t, err, "[PG] model is nil")
	_, err = BuildInsert(m)
	require.EqualError(t, err, "[PG] model is nil")
	_, err = BuildUpdate(m)
	require.EqualError(t, err, "[PG] model is nil")
	_, err = BuildDelete(m)
	require.EqualError(t, err, "[PG] model is nil")
}

// defaultsModel has columns with defaults, the pointer fields can insert zero values.
type defaultsModel struct {
	model.CreateOnly[model.ID]
	Name     string
	Priority int
	Active   bool
	Enabled  *bool
	Tags     []string
}

func (m *defaultsModel) TableName() string { return "defaults_models" }

func TestCreateModelAppliesColumnDefaults(t *testing.T) {
	ctx := context.Background()
	_, err := DB.Exec(ctx, `CREATE TABLE IF NOT EXISTS defaults_models (
		"id" Bigserial PRIMARY KEY,
		"name" Text NOT NULL DEFAULT 'unnamed',
		"priority" Int NOT NULL DEFAULT 5,
		"active" Bool NOT NULL DEFAULT true,
		"enabled" Bool NOT NULL DEFAULT true,
		"tags" Text[] NOT NULL DEFAULT '{default}',
		"created" Timestamptz)`)
	require.NoError(t, err)
	require.NoError(t, ClearTables("defaults_models"))

	// the zero values get the column defaults, also the NOT NULL columns
	created, err := CreateModel(&defaultsModel{})
	require.NoError(t, err)
	assert.Equal(t, "unnamed", created.Name)
	assert.Equal(t, 5, created.Priority)
	assert.True(t, created.Active)
	require.NotNil(t, created.Enabled)
	assert.True(t, *created.Enabled)
	assert.Equal(t, []string{"default"}, created.Tags)
	assert.False(t, created.Created.IsZero())

	// the set values are inserted, a pointer inserts a zero value
	disabled := false
	created, err = CreateModel(&defaultsModel{Name: "john", Priority: 1, Enabled: &disabled, Tags: []string{}})
	require.NoError(t, err)
	assert.Equal(t, "john", created.Name)
	assert.Equal(t, 1, created.Priority)
	assert.True(t, created.Active, "a false bool is zero, it gets the default")
	assert.False(t, *created.Enabled)
	assert.Equal(t, []string{}, created.Tags)
}
