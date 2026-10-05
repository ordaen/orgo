package pg

import (
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/ordaen/orgo/model"
)

type fieldInfo struct {
	name        string
	quoted      string // the quoted column name
	idx         []int
	fieldType   reflect.Type
	fieldKind   reflect.Kind
	isUpdatedAt bool
	isCreatedAt bool
}

type modelInfo struct {
	name string
	// tableName and quotedTable are the table of the model type, empty when it is not a Model.
	tableName   string
	quotedTable string
	// typeName is the model type name without the package, or the result of ModelType when the type has it.
	// It is empty when ModelType panics on a new model.
	typeName string
	fields   []fieldInfo
	// createOnly is set for the models embedding model.CreateOnly, they cannot be updated.
	createOnly bool
}

// getField returns the field mapped to the column name, or nil if there is none.
// The fields are searched in order, models have too few fields for a map to be faster.
func (mi *modelInfo) getField(name string) *fieldInfo {
	for i := range mi.fields {
		if mi.fields[i].name == name {
			return &mi.fields[i]
		}
	}
	return nil
}

type structCache struct {
	types sync.Map // reflect.Type -> *modelInfo
}

var globalCache = &structCache{}

func (sc *structCache) getModelInfo(t reflect.Type) *modelInfo {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if info, ok := sc.types.Load(t); ok {
		return info.(*modelInfo)
	}
	// concurrent callers may build the same info, LoadOrStore keeps the first one
	info, _ := sc.types.LoadOrStore(t, newModelInfo(t))
	return info.(*modelInfo)
}

func newModelInfo(t reflect.Type) *modelInfo {
	info := &modelInfo{name: t.Name()}
	if name, ok := typeTableName(t); ok {
		info.tableName, info.quotedTable = name, QuoteTable(name)
	}
	info.typeName = typeModelType(t)
	if t.Kind() == reflect.Struct {
		info.fields = dedupeFields(extractFields(t, nil, nil, map[reflect.Type]bool{}))
		info.createOnly = embedsCreateOnly(t, map[reflect.Type]bool{})
	}
	for i := range info.fields {
		info.fields[i].quoted = QuoteIdent(info.fields[i].name)
	}
	return info
}

// extractFields collects the mapped fields of t, going into embedded structs.
// visiting holds the embedded types on the current path to stop on recursive embedding.
func extractFields(t reflect.Type, baseIdx []int, list []fieldInfo, visiting map[reflect.Type]bool) []fieldInfo {
	visiting[t] = true
	defer delete(visiting, t)

	for field := range t.Fields() {
		// get tag from the field
		tag := field.Tag.Get("db")
		if tag == "-" {
			continue
		}
		// Calculate the full index path for the current field.
		idx := slices.Concat(baseIdx, field.Index)

		// If the field is embedded (Embedded / Anonymous struct)
		if field.Anonymous {
			embedded := field.Type
			isPtr := embedded.Kind() == reflect.Pointer
			if isPtr {
				embedded = embedded.Elem()
			}
			// Exported fields of an unexported embedded struct are promoted and settable,
			// but a nil unexported embedded pointer cannot be allocated, so it is skipped.
			if embedded.Kind() == reflect.Struct && (field.IsExported() || !isPtr) {
				if !visiting[embedded] {
					// Go deeper, passing the accumulated index path
					list = extractFields(embedded, idx, list, visiting)
				}
				continue
			}
		}
		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		info := fieldInfo{
			name:      toSnakeCase(field.Name),
			idx:       idx,
			fieldType: field.Type,
			fieldKind: field.Type.Kind(),
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name != "" {
			info.name = name
		}
		for opt := range strings.SplitSeq(opts, ",") {
			switch opt {
			case "updated_at":
				info.isUpdatedAt = true
			case "created_at":
				info.isCreatedAt = true
			}
		}
		list = append(list, info)
	}

	return list
}

// typeTableName returns the table of the model type t, calling TableName on a new model.
// It returns false when t is not a Model or TableName panics on a new model.
func typeTableName(t reflect.Type) (name string, ok bool) {
	return callOnNew(t, Model.TableName)
}

// ModelTyper is implemented by the models with a custom type name, returned by ModelType.
// ModelType must return the same name for every model of a type: it is read once per type, on a new model.
type ModelTyper interface {
	ModelType() string
}

// typeModelType returns the type name of t, calling ModelType on a new model when t has it,
// or the type name without the package and the type arguments otherwise.
// It returns an empty name when ModelType panics on a new model.
func typeModelType(t reflect.Type) string {
	if name, ok := callOnNew(t, ModelTyper.ModelType); ok {
		return name
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[ModelTyper]()) {
		return ""
	}
	name, _, _ := strings.Cut(t.Name(), "[")
	return name
}

// callOnNew calls method on a new value of t, or on the pointer to it, when one of them implements I.
// It returns false when neither implements I or method panics.
func callOnNew[I any](t reflect.Type, method func(I) string) (res string, ok bool) {
	defer func() {
		if recover() != nil {
			res, ok = "", false
		}
	}()
	p := reflect.New(t)
	if v, is := p.Interface().(I); is {
		return method(v), true
	}
	if v, is := p.Elem().Interface().(I); is {
		return method(v), true
	}
	return "", false
}

// ModelType returns the type name of m without the package, like "Admin" for models.Admin,
// or the result of its ModelType method when it has one. The name is read once per type.
func ModelType(m Model) string {
	if name := globalCache.getModelInfo(reflect.TypeOf(m)).typeName; name != "" {
		return name
	}
	// ModelType panicked on a new model
	if typer, ok := m.(ModelTyper); ok {
		return typer.ModelType()
	}
	return ""
}

// table returns the quoted table of m, the table of its type unless it could not be read from a new model.
func (mi *modelInfo) table(m Model) string {
	if mi.quotedTable != "" {
		return mi.quotedTable
	}
	return QuoteTable(m.TableName())
}

// modelPkgPath is the path of the model package, the package of model.CreateOnly.
var modelPkgPath = reflect.TypeFor[model.ID]().PkgPath()

// embedsCreateOnly reports whether the struct t embeds model.CreateOnly, directly or through embedded structs.
// visited holds the checked embedded types, to stop on recursive embedding.
func embedsCreateOnly(t reflect.Type, visited map[reflect.Type]bool) bool {
	visited[t] = true
	for field := range t.Fields() {
		if !field.Anonymous {
			continue
		}
		embedded := field.Type
		if embedded.Kind() == reflect.Pointer {
			embedded = embedded.Elem()
		}
		if embedded.Kind() != reflect.Struct || visited[embedded] {
			continue
		}
		// the name of a generic type includes its type arguments: "CreateOnly[github.com/ordaen/orgo/model.ID]"
		if embedded.PkgPath() == modelPkgPath && strings.HasPrefix(embedded.Name(), "CreateOnly[") {
			return true
		}
		if embedsCreateOnly(embedded, visited) {
			return true
		}
	}
	return false
}

// dedupeFields resolves fields mapped to the same column name like Go resolves promoted fields:
// the shallowest field wins, and at the same depth the first declared one wins.
// The winner takes the position of the first occurrence.
func dedupeFields(fields []fieldInfo) []fieldInfo {
	pos := make(map[string]int, len(fields))
	res := fields[:0]
	for _, f := range fields {
		if i, ok := pos[f.name]; ok {
			if len(f.idx) < len(res[i].idx) {
				res[i] = f
			}
			continue
		}
		pos[f.name] = len(res)
		res = append(res, f)
	}
	return res
}

func toSnakeCase(str string) string {
	if str == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(str) + 4) // Small buffer to avoid allocations

	for i := 0; i < len(str); i++ {
		c := str[i]

		// If the letter is uppercase
		if c >= 'A' && c <= 'Z' {
			// Add underscore if it's not the start of the string,
			// and if the previous character was lowercase (e.g. from ClientID -> tI -> t_i)
			// OR if the next character is lowercase and the previous character is uppercase (e.g. from IDName -> ID_Name)
			if i > 0 {
				prev := str[i-1]
				nextIsLower := i+1 < len(str) && str[i+1] >= 'a' && str[i+1] <= 'z'
				prevIsUpper := prev >= 'A' && prev <= 'Z'

				if (prev >= 'a' && prev <= 'z') || (nextIsLower && !prevIsUpper) {
					b.WriteByte('_')
				}
			}
			// Convert to lowercase
			b.WriteByte(c + ('a' - 'A'))
		} else {
			b.WriteByte(c)
		}
	}

	return b.String()
}
