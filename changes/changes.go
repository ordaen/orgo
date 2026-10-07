// Package changes lists the differences between two values as field paths with their old and new values,
// for audit logs and change notifications.
package changes

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"
)

var timeType = reflect.TypeFor[time.Time]()

// MaskChangesFor makes ValuesDiff report the changes of T, or of the type T points to, with mask
// as both the old and the new value, so secrets are not exposed. A masked struct or map is compared
// as a whole instead of field by field.
func MaskChangesFor[T any](mask string) {
	maskedTypes.Store(baseType(reflect.TypeFor[T]()), mask)
}

// UnmaskChangesFor reverts MaskChangesFor for T.
func UnmaskChangesFor[T any]() {
	maskedTypes.Delete(baseType(reflect.TypeFor[T]()))
}

// maskedTypes maps a reflect.Type to its mask.
var maskedTypes sync.Map

func maskFor(t reflect.Type) (string, bool) {
	mask, ok := maskedTypes.Load(t)
	if !ok {
		return "", false
	}
	return mask.(string), true
}

func baseType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// Change is a changed key with its old and new values.
type Change struct {
	// Key is the changed key, a dot separated field path.
	Key string `json:"key"`
	// From is the old value.
	From string `json:"from"`
	// To is the new value.
	To string `json:"to"`
}

// UnmarshalJSON reads a change as an object, or as the [key, from, to] array of the older stored changes.
func (c *Change) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		var a [3]string
		if err := json.Unmarshal(data, &a); err != nil {
			return err
		}
		*c = Change{Key: a[0], From: a[1], To: a[2]}
		return nil
	}
	// The alias has no UnmarshalJSON, so decoding into it does not recurse.
	type change Change
	return json.Unmarshal(data, (*change)(c))
}

// Changes is a list of changes with unique keys.
type Changes []Change

// Contains reports whether any of the keys is changed.
func (c Changes) Contains(keys ...string) bool {
	return slices.ContainsFunc(c, func(ch Change) bool {
		return slices.Contains(keys, ch.Key)
	})
}

// Keys returns the changed keys in their order, each once.
func (c Changes) Keys() []string {
	keys := make([]string, 0, len(c))
	for _, ch := range c {
		if !slices.Contains(keys, ch.Key) {
			keys = append(keys, ch.Key)
		}
	}
	return keys
}

// Get returns the old and new values of the key, or empty strings when the key is not changed.
func (c Changes) Get(key string) (from, to string) {
	for _, ch := range c {
		if ch.Key == key {
			return ch.From, ch.To
		}
	}
	return "", ""
}

// Add adds a change of the key with the values formatted by Stringify, unless the key is already changed.
func (c *Changes) Add(key string, from, to any) {
	if c.Contains(key) {
		return
	}
	*c = append(*c, Change{Key: key, From: Stringify(from), To: Stringify(to)})
}

// ValuesDiff returns the changes from v1 to v2, field is the key of the values. Pointers and interfaces
// are followed, structs and maps are compared field by field and key by key with the keys joined by dots,
// like "field.Name" or "field.key". Fields of embedded structs are listed as fields of the outer struct,
// unexported fields and fields tagged db:"-" are skipped. Other values are compared as formatted by Stringify.
func ValuesDiff(v1, v2 reflect.Value, field string) Changes {
	var res Changes
	diff(&res, v1, v2, field)
	return res
}

func diff(res *Changes, v1, v2 reflect.Value, key string) {
	v1, v2 = indirect(v1), indirect(v2)
	if !v1.IsValid() && !v2.IsValid() {
		return
	}
	// A nil struct or map is compared as its zero value, other nil values as an empty string.
	if !v1.IsValid() {
		if isComposite(v2.Type()) {
			v1 = reflect.Zero(v2.Type())
		}
	} else if !v2.IsValid() {
		if isComposite(v1.Type()) {
			v2 = reflect.Zero(v1.Type())
		}
	}

	if mask, ok := maskFor(typeOf(v1, v2)); ok {
		if !leafEqual(v1, v2) {
			*res = append(*res, Change{Key: key, From: mask, To: mask})
		}
		return
	}
	if v1.IsValid() && v2.IsValid() && v1.Type() == v2.Type() {
		switch t := v1.Type(); {
		case t.Kind() == reflect.Map:
			diffMaps(res, v1, v2, key)
			return
		case t.Kind() == reflect.Struct && t != timeType:
			diffStructs(res, v1, v2, key)
			return
		}
	}
	if !leafEqual(v1, v2) {
		*res = append(*res, Change{Key: key, From: stringVal(v1), To: stringVal(v2)})
	}
}

func diffStructs(res *Changes, v1, v2 reflect.Value, key string) {
	for f := range v1.Type().Fields() {
		if f.Tag.Get("db") == "-" {
			continue
		}
		fieldKey := key + "." + f.Name
		if f.Anonymous && baseType(f.Type).Kind() == reflect.Struct {
			// The exported fields of an embedded struct are promoted, even when the struct is unexported.
			fieldKey = key
		} else if !f.IsExported() {
			continue
		}
		diff(res, v1.FieldByIndex(f.Index), v2.FieldByIndex(f.Index), fieldKey)
	}
}

func diffMaps(res *Changes, v1, v2 reflect.Value, key string) {
	keys := v1.MapKeys()
	for _, k := range v2.MapKeys() {
		if !v1.MapIndex(k).IsValid() {
			keys = append(keys, k)
		}
	}
	names := make([]string, len(keys))
	order := make([]int, len(keys))
	for i, k := range keys {
		names[i] = stringVal(k)
		order[i] = i
	}
	// Sort the keys so the changes are listed in the same order every time.
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(names[a], names[b]) })
	for _, i := range order {
		diff(res, v1.MapIndex(keys[i]), v2.MapIndex(keys[i]), key+"."+names[i])
	}
}

func isComposite(t reflect.Type) bool {
	return t.Kind() == reflect.Map || (t.Kind() == reflect.Struct && t != timeType)
}

func typeOf(v1, v2 reflect.Value) reflect.Type {
	if v1.IsValid() {
		return v1.Type()
	}
	return v2.Type()
}

// leafEqual reports whether v1 and v2 format to the same string, deeply equal values skip the formatting.
func leafEqual(v1, v2 reflect.Value) bool {
	if v1.IsValid() && v2.IsValid() && v1.Type() == v2.Type() && v1.CanInterface() && v2.CanInterface() {
		if v1.Type() == timeType {
			return v1.Interface().(time.Time).Equal(v2.Interface().(time.Time))
		}
		if reflect.DeepEqual(v1.Interface(), v2.Interface()) {
			return true
		}
	}
	return stringVal(v1) == stringVal(v2)
}

// Stringify formats a value for a Change: nil is empty, a time is in RFC 3339, a fmt.Stringer uses String,
// strings, integers and booleans are written as they are, an empty slice is "[]" and other values are indented JSON.
func Stringify(e any) string {
	return stringVal(reflect.ValueOf(e))
}

func stringVal(v reflect.Value) string {
	v = indirect(v)
	if !v.IsValid() {
		return ""
	}
	if v.CanInterface() {
		switch i := v.Interface().(type) {
		case time.Time:
			return i.Format(time.RFC3339Nano)
		case fmt.Stringer:
			return i.String()
		}
	}

	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Slice:
		if v.Len() == 0 {
			return "[]"
		}
	}

	if !v.CanInterface() {
		return fmt.Sprint(v)
	}
	iface := v.Interface()
	b, err := json.MarshalIndent(iface, "  ", "  ")
	if err != nil {
		return fmt.Sprint(iface)
	}
	return string(b)
}

// indirect follows pointers and interfaces, it returns an invalid Value for nil.
func indirect(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		v = v.Elem()
	}
	return v
}
