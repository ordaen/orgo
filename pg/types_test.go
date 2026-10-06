package pg

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "ClientID", expected: "client_id"},
		{input: "CreatedAt", expected: "created_at"},
		{input: "Name", expected: "name"},
	}

	for _, test := range tests {
		assert.Equal(t, toSnakeCase(test.input), test.expected, "input: %s", test.input)
	}
}

type TestModel struct {
	model.Base[model.ID]
	FirstName string `db:""`
	LastName  string `db:"last"`
	Email     string `db:"email_field"`
	Col1      string `db:",col1_field"`
}

func (m *TestModel) TableName() string {
	return "test_models"
}

type CreateOnlyModel struct {
	model.CreateOnly[model.ID]
	FirstName string
}

func (m *CreateOnlyModel) TableName() string {
	return "create_only_models"
}

func TestGetModelInfo(t *testing.T) {
	info := globalCache.getModelInfo(reflect.TypeFor[*TestModel]())
	assert.Equal(t, "TestModel", info.name)
	assert.False(t, info.createOnly)
	assert.Equal(t, []fieldInfo{
		{name: "id", quoted: `"id"`, idx: []int{0, 0}, fieldType: reflect.TypeFor[model.ID](), fieldKind: reflect.Int},
		{name: "created", quoted: `"created"`, idx: []int{0, 1}, fieldType: reflect.TypeFor[time.Time](), fieldKind: reflect.Struct, isCreatedAt: true},
		{name: "updated", quoted: `"updated"`, idx: []int{0, 2}, fieldType: reflect.TypeFor[time.Time](), fieldKind: reflect.Struct, isUpdatedAt: true},
		{name: "first_name", quoted: `"first_name"`, idx: []int{1}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
		{name: "last", quoted: `"last"`, idx: []int{2}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
		{name: "email_field", quoted: `"email_field"`, idx: []int{3}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
		{name: "col1", quoted: `"col1"`, idx: []int{4}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
	}, info.fields)

	info = globalCache.getModelInfo(reflect.TypeFor[*CreateOnlyModel]())
	assert.Equal(t, "CreateOnlyModel", info.name)
	assert.True(t, info.createOnly)
	assert.Equal(t, []fieldInfo{
		{name: "id", quoted: `"id"`, idx: []int{0, 0}, fieldType: reflect.TypeFor[model.ID](), fieldKind: reflect.Int},
		{name: "created", quoted: `"created"`, idx: []int{0, 1}, fieldType: reflect.TypeFor[time.Time](), fieldKind: reflect.Struct, isCreatedAt: true},
		{name: "first_name", quoted: `"first_name"`, idx: []int{1}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
	}, info.fields)
}

type typesInner struct {
	Name  string
	Email string
	Deep  string
}

type typesMiddle struct {
	typesInner
	Deep string `db:"deep"`
}

type typesShadow struct {
	typesMiddle // unexported embedded struct: exported fields are promoted
	Name        string
}

func TestGetModelInfoShadowing(t *testing.T) {
	info := globalCache.getModelInfo(reflect.TypeFor[typesShadow]())
	assert.Equal(t, []fieldInfo{
		// outer Name (depth 1) wins over typesInner.Name (depth 3), but keeps the first position
		{name: "name", quoted: `"name"`, idx: []int{1}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
		{name: "email", quoted: `"email"`, idx: []int{0, 0, 1}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
		// typesMiddle.Deep (depth 2) wins over typesInner.Deep (depth 3)
		{name: "deep", quoted: `"deep"`, idx: []int{0, 1}, fieldType: reflect.TypeFor[string](), fieldKind: reflect.String},
	}, info.fields)
}

type typesUnexportedPtr struct {
	*typesInner // cannot be allocated through reflection, skipped
	ID          int
}

type TypesRecursive struct {
	*TypesRecursive
	ID int
}

func TestGetModelInfoEmbeddedPointers(t *testing.T) {
	info := globalCache.getModelInfo(reflect.TypeFor[typesUnexportedPtr]())
	assert.Equal(t, []fieldInfo{
		{name: "id", quoted: `"id"`, idx: []int{1}, fieldType: reflect.TypeFor[int](), fieldKind: reflect.Int},
	}, info.fields)

	// recursive embedding must not recurse forever
	info = globalCache.getModelInfo(reflect.TypeFor[*TypesRecursive]())
	assert.Equal(t, []fieldInfo{
		{name: "id", quoted: `"id"`, idx: []int{1}, fieldType: reflect.TypeFor[int](), fieldKind: reflect.Int},
	}, info.fields)
}

func TestGetModelInfoNotStruct(t *testing.T) {
	info := globalCache.getModelInfo(reflect.TypeFor[int]())
	assert.Empty(t, info.fields)
	assert.Nil(t, info.getField("id"))
}

func TestGetField(t *testing.T) {
	info := globalCache.getModelInfo(reflect.TypeFor[TestModel]())
	f := info.getField("last")
	if assert.NotNil(t, f) {
		assert.Equal(t, []int{2}, f.idx)
		// must point into info.fields, not to a copy
		assert.Same(t, &info.fields[4], f)
	}
	assert.Nil(t, info.getField("last_name"))
	assert.Nil(t, info.getField("missing"))
}

func TestGetModelInfoConcurrent(t *testing.T) {
	type concurrentModel struct{ ID int }
	tp := reflect.TypeFor[concurrentModel]()

	var wg sync.WaitGroup
	infos := make([]*modelInfo, 20)
	for i := range infos {
		wg.Go(func() { infos[i] = globalCache.getModelInfo(tp) })
	}
	wg.Wait()
	for _, info := range infos {
		assert.Same(t, infos[0], info)
	}
}

// typesPlain has no timestamps, typesCreatedOnly has a created column without embedding model.CreateOnly.
type typesPlain struct {
	ID   model.ID
	Name string
}

type typesCreatedOnly struct {
	ID      model.ID
	Created time.Time `db:"created,created_at"`
}

// typesBase embeds model.CreateOnly, typesNestedCreateOnly embeds it through typesBase.
type typesBase struct {
	*model.CreateOnly[model.UUID]
}

type typesNestedCreateOnly struct {
	typesBase
	Name string
}

func TestGetModelInfoCreateOnly(t *testing.T) {
	// only the models embedding model.CreateOnly are create only, the columns do not matter
	assert.False(t, globalCache.getModelInfo(reflect.TypeFor[typesPlain]()).createOnly)
	assert.False(t, globalCache.getModelInfo(reflect.TypeFor[typesCreatedOnly]()).createOnly)
	assert.True(t, globalCache.getModelInfo(reflect.TypeFor[typesNestedCreateOnly]()).createOnly)
	assert.True(t, globalCache.getModelInfo(reflect.TypeFor[CreateOnlyModel]()).createOnly)
	assert.False(t, globalCache.getModelInfo(reflect.TypeFor[TestModel]()).createOnly)
	assert.False(t, globalCache.getModelInfo(reflect.TypeFor[*TypesRecursive]()).createOnly)
}

// typesPanickingTable reads its table from a field, so TableName panics on a new model.
type typesPanickingTable struct {
	model.Base[model.ID]
	Shard *string
}

func (m *typesPanickingTable) TableName() string {
	return "shard_" + *m.Shard
}

func TestGetModelInfoTable(t *testing.T) {
	info := globalCache.getModelInfo(reflect.TypeFor[*builderOrderModel]())
	assert.Equal(t, "orgo.order", info.tableName)
	assert.Equal(t, `"orgo"."order"`, info.quotedTable)

	// the types that are not models, or panic on a new model, have no table and quote it per call
	assert.Empty(t, globalCache.getModelInfo(reflect.TypeFor[mapperAllTypes]()).quotedTable)
	info = globalCache.getModelInfo(reflect.TypeFor[*typesPanickingTable]())
	assert.Empty(t, info.quotedTable)
	shard := "eu"
	assert.Equal(t, `"shard_eu"`, info.table(&typesPanickingTable{Shard: &shard}))
	query, err := BuildDelete(&typesPanickingTable{Base: model.Base[model.ID]{ID: 1}, Shard: &shard})
	require.NoError(t, err)
	assert.Equal(t, `DELETE FROM "shard_eu" WHERE "id" = $1 RETURNING *`, query.SQL)

	// Query uses the table of the type, or of the model when the type has none
	sql, _ := Query(&builderOrderModel{}).build("*", true)
	assert.Equal(t, `SELECT * FROM "orgo"."order"`, sql)
	var m Model = &builderOrderModel{}
	sql, _ = Query(m).build("*", true)
	assert.Equal(t, `SELECT * FROM "orgo"."order"`, sql)
}

// typesCustomType has a custom type name.
type typesCustomType struct {
	model.Base[model.ID]
}

func (m *typesCustomType) TableName() string { return "custom" }
func (m *typesCustomType) ModelType() string { return "Custom" }

// typesPanickingType reads its type name from a field, so ModelType panics on a new model.
type typesPanickingType struct {
	model.Base[model.ID]
	Kind *string
}

func (m *typesPanickingType) TableName() string { return "kinds" }
func (m *typesPanickingType) ModelType() string { return *m.Kind }

// typesGeneric is a generic model, its type name has no type arguments.
type typesGeneric[T any] struct {
	model.Base[model.ID]
	Value T
}

func (m *typesGeneric[T]) TableName() string { return "generic" }

func TestModelType(t *testing.T) {
	// the type name has no package, or is the result of ModelType
	assert.Equal(t, "TestModel", globalCache.getModelInfo(reflect.TypeFor[*TestModel]()).typeName)
	assert.Equal(t, "TestModel", ModelType(&TestModel{}))
	assert.Equal(t, "Custom", ModelType(&typesCustomType{}))
	assert.Equal(t, "typesGeneric", ModelType(&typesGeneric[model.ID]{}))

	// a type whose ModelType panics on a new model calls it per model
	assert.Empty(t, globalCache.getModelInfo(reflect.TypeFor[*typesPanickingType]()).typeName)
	kind := "Special"
	assert.Equal(t, "Special", ModelType(&typesPanickingType{Kind: &kind}))
}

func TestHasColumn(t *testing.T) {
	assert.True(t, HasColumn[*baseModel]("name"))
	assert.True(t, HasColumn[baseModel]("id"))
	assert.True(t, HasColumn[*baseModel]("created"), "the columns of the embedded structs")
	assert.False(t, HasColumn[*baseModel]("missing"))
	assert.False(t, HasColumn[*baseModel]("name; DROP TABLE x"))
	assert.False(t, HasColumn[int]("id"))
}
