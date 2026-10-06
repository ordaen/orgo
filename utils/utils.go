// Package utils has the small helpers of the orgo packages: random strings, string sanitizing and reflection.
package utils

import "reflect"

// TypeFields returns all fields indexes in structure
func TypeFields(t reflect.Type, idx []int) (res [][]int) {
	for i := 0; i < t.NumField(); i++ {
		fd := t.Field(i)
		if fd.PkgPath != "" && !fd.Anonymous {
			continue
		}

		idx1 := append(idx, fd.Index...)
		if fd.Anonymous && fd.Type.Kind() == reflect.Struct {
			res = append(res, TypeFields(fd.Type, idx1)...)
			continue
		}
		idx2 := make([]int, len(idx1))
		copy(idx2, idx1)
		res = append(res, idx2)
	}
	return
}
