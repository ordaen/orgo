package pg

import (
	"reflect"

	"github.com/jackc/pgx/v5"
)

// MapRows scans all rows into a slice of T. T must be a struct or a pointer to a struct.
func MapRows[T any](rows pgx.Rows) ([]T, error) {
	return mapRows[T](rows, 0)
}

// MapRowsLimited scans at most limit rows into a slice of T. A limit <= 0 means no limit.
func MapRowsLimited[T any](rows pgx.Rows, limit int) ([]T, error) {
	return mapRows[T](rows, limit)
}

func mapRows[T any](rows pgx.Rows, limit int) ([]T, error) {
	defer rows.Close()

	tType := reflect.TypeFor[T]()
	isPtr := tType.Kind() == reflect.Pointer
	if isPtr {
		tType = tType.Elem()
	}
	if tType.Kind() != reflect.Struct {
		return nil, Errorf("map rows: %s is not a struct", tType)
	}
	info := globalCache.getModelInfo(tType)

	// resolve column -> field index path once, the result is the same for every row. nil when the column has no field.
	desc := rows.FieldDescriptions()
	plans := make([][]int, len(desc))
	for i, fd := range desc {
		if field := info.getField(fd.Name); field != nil {
			plans[i] = field.idx
		}
	}

	var results []T
	if limit > 0 {
		results = make([]T, 0, limit)
	}
	// nil entries tell pgx to skip the columns without a field, and the NULL values:
	// the fields of a new record already have their zero values, also those that cannot represent NULL
	scanArgs := make([]any, len(desc))

	for (limit <= 0 || len(results) < limit) && rows.Next() {
		var item T
		var v reflect.Value
		if isPtr {
			p := reflect.New(tType)
			item = p.Interface().(T)
			v = p.Elem()
		} else {
			v = reflect.ValueOf(&item).Elem()
		}

		raw := rows.RawValues()
		for i, idx := range plans {
			if idx == nil || raw[i] == nil {
				scanArgs[i] = nil
				continue
			}
			scanArgs[i] = fieldByIndex(v, idx).Addr().Interface()
		}

		// pgx converts the values: numbers, time, arrays, sql.Scanner, jsonb into structs/maps/slices, etc.
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, Errorf("scan: %w", err)
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, Errorf("rows iteration: %w", err)
	}

	return results, nil
}

// fieldByIndex is like reflect.Value.FieldByIndex, but allocates nil embedded struct pointers along the path.
func fieldByIndex(v reflect.Value, idx []int) reflect.Value {
	for i, x := range idx {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}
