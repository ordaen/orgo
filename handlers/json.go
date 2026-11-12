package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
)

// ModelWithExtraFields is a model with computed fields added to its JSON in the responses, like the name of
// a related record. ExtraField returns the value of a name of ExtraFields, a nil value is not added. When the
// request has fields, only the extra fields in them are computed.
type ModelWithExtraFields interface {
	ExtraFields() []string
	ExtraField(name string) any
}

var extraFieldsType = reflect.TypeFor[ModelWithExtraFields]()

// EncodeData returns the JSON of data, a value or a slice of values, with the extra fields of the values
// implementing ModelWithExtraFields. With fields, the JSON objects have only the id and the fields.
func EncodeData(data any, fields []string) ([]byte, error) {
	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().Kind() == reflect.Slice {
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array || v.Type().Elem().Kind() == reflect.Uint8 {
		return encodeItem(data, fields)
	}
	elem := v.Type().Elem()
	if len(fields) == 0 && !elem.Implements(extraFieldsType) && !reflect.PointerTo(elem).Implements(extraFieldsType) {
		return json.Marshal(data)
	}
	if v.Kind() == reflect.Slice && v.IsNil() {
		return []byte("null"), nil
	}
	var b bytes.Buffer
	b.WriteByte('[')
	for i := range v.Len() {
		if i > 0 {
			b.WriteByte(',')
		}
		item := v.Index(i)
		if item.Kind() != reflect.Pointer && item.CanAddr() {
			item = item.Addr()
		}
		ib, err := encodeItem(item.Interface(), fields)
		if err != nil {
			return nil, err
		}
		b.Write(ib)
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

// encodeItem returns the JSON of the value with its extra fields, only the id and the fields when there are fields.
func encodeItem(item any, fields []string) ([]byte, error) {
	b, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	extra, _ := item.(ModelWithExtraFields)
	if extra != nil {
		if v := reflect.ValueOf(item); v.Kind() == reflect.Pointer && v.IsNil() {
			extra = nil
		}
	}
	if len(fields) == 0 && extra == nil || len(b) == 0 || b[0] != '{' {
		return b, nil
	}
	return filterObject(b, fields, extra)
}

// filterObject returns the JSON object obj with only the id and the fields when there are fields, and with the
// extra fields. The order of the keys is kept.
func filterObject(obj []byte, fields []string, extra ModelWithExtraFields) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var (
		b    bytes.Buffer
		keys []string
	)
	b.WriteByte('{')
	add := func(key string, val []byte) {
		if len(keys) > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(key)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(val)
		keys = append(keys, key)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		if len(fields) == 0 || key == "id" || slices.Contains(fields, key) {
			add(key, val)
		}
	}
	if extra != nil {
		for _, name := range extra.ExtraFields() {
			if len(fields) > 0 && !slices.Contains(fields, name) || slices.Contains(keys, name) {
				continue
			}
			v := extra.ExtraField(name)
			if v == nil {
				continue
			}
			vb, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("extra field %q: %w", name, err)
			}
			add(name, vb)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// DecodeRecord sets the fields of rec from the JSON object body. It returns the changes of the fields, keyed by
// their JSON names, and the columns of the changed fields, for logger.UpdateRecord. The fields with the json
// option readonly, like `json:"total,readonly"`, are never set, the fields with createonly only when create is true,
// and neither are the id and the created and updated timestamps. The other keys of body are ignored.
//
// Each value is decoded into a new value of its field, so the slices and maps of rec are not modified, they can
// be shared with a cached record: a field is replaced, a JSON object does not merge into the struct or map of rec.
// It returns an ErrorBadRequest when body is not a JSON object, an ErrorInvalidAttribute for an invalid value.
func DecodeRecord[T model.Model](body []byte, rec T, create bool) (changes.Changes, []string, error) {
	v := reflect.ValueOf(rec)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("decode record: %T is not a pointer to a struct", rec)
	}
	v = v.Elem()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		return nil, nil, ErrorBadRequest("the request body is not a JSON object")
	}
	var (
		ch   changes.Changes
		cols []string
	)
	for _, f := range jsonFieldsOf(v.Type()) {
		raw, ok := obj[f.name]
		if !ok || f.protected || f.readonly || f.createonly && !create {
			continue
		}
		field, err := v.FieldByIndexErr(f.idx)
		if err != nil {
			// behind a nil embedded pointer
			continue
		}
		val := reflect.New(f.typ)
		if f.quoted && string(raw) != "null" {
			// the ,string option: the value is in a JSON string
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, nil, ErrorInvalidAttribute(f.name, f.name+" must be a string")
			}
			raw = []byte(s)
		}
		if err := json.Unmarshal(raw, val.Interface()); err != nil {
			return nil, nil, ErrorInvalidAttribute(f.name, f.name+" "+decodeErrorDetail(err))
		}
		diff := changes.ValuesDiff(field, val.Elem(), f.name)
		if len(diff) == 0 {
			continue
		}
		field.Set(val.Elem())
		ch = append(ch, diff...)
		if col, ok := pg.FieldColumn[T](f.idx); ok {
			cols = append(cols, col)
		}
	}
	return ch, cols, nil
}

// decodeErrorDetail returns the detail of an error decoding a value.
func decodeErrorDetail(err error) string {
	if te, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return "must be " + jsonKind(te.Type)
	}
	return "is invalid: " + err.Error()
}

// jsonKind returns the JSON kind of the values of t.
func jsonKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Slice, reflect.Array:
		return "an array"
	case reflect.Map, reflect.Struct:
		return "an object"
	case reflect.Pointer:
		return jsonKind(t.Elem())
	}
	return "a " + t.String()
}

// jsonField is a field of a struct encoded in JSON.
type jsonField struct {
	name  string
	idx   []int
	typ   reflect.Type
	depth int
	// readonly and createonly are the options of the json tag
	readonly   bool
	createonly bool
	// quoted is set by the string option: the value is encoded in a JSON string
	quoted bool
	// protected is set for the id and the created and updated timestamps, never decoded
	protected bool
}

var jsonFieldsCache sync.Map // reflect.Type -> []jsonField

// jsonFieldsOf returns the JSON fields of the struct type t, with the fields of the embedded structs without
// a JSON name, like encoding/json. Of the fields with the same name the least nested one is kept.
func jsonFieldsOf(t reflect.Type) []jsonField {
	if v, ok := jsonFieldsCache.Load(t); ok {
		return v.([]jsonField)
	}
	all := collectJSONFields(t, nil, 0, nil, map[reflect.Type]bool{})
	var res []jsonField
	for _, f := range all {
		i := slices.IndexFunc(res, func(r jsonField) bool { return r.name == f.name })
		switch {
		case i < 0:
			res = append(res, f)
		case f.depth < res[i].depth:
			res[i] = f
		}
	}
	jsonFieldsCache.Store(t, res)
	return res
}

func collectJSONFields(t reflect.Type, base []int, depth int, list []jsonField, visiting map[reflect.Type]bool) []jsonField {
	visiting[t] = true
	defer delete(visiting, t)
	for sf := range t.Fields() {
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		idx := slices.Concat(base, sf.Index)
		if sf.Anonymous && name == "" {
			et := sf.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				if !visiting[et] {
					list = collectJSONFields(et, idx, depth+1, list, visiting)
				}
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		f := jsonField{name: name, idx: idx, typ: sf.Type, depth: depth}
		for opt := range strings.SplitSeq(opts, ",") {
			switch opt {
			case "readonly":
				f.readonly = true
			case "createonly":
				f.createonly = true
			case "string":
				f.quoted = true
			}
		}
		_, dbOpts, _ := strings.Cut(sf.Tag.Get("db"), ",")
		f.protected = name == "id" || slices.ContainsFunc(strings.Split(dbOpts, ","), func(o string) bool {
			return o == "created_at" || o == "updated_at"
		})
		list = append(list, f)
	}
	return list
}
