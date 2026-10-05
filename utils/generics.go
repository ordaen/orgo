package utils

import "reflect"

// TypeCreate creates new type with reflect
func TypeCreate[T any]() T {
	if typ := reflect.TypeFor[T](); typ.Kind() == reflect.Pointer {
		return reflect.New(typ.Elem()).Interface().(T)
	}
	var zero T
	return zero
}
