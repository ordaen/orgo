package gql

import (
	"context"
	"fmt"

	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/utils"

	"github.com/ordaen/orgo/changes"
)

// UpdateIfExists sets from to the value of to when to is not nil and differs, and adds the change of key
// to changedFields. It reports whether from was changed.
func UpdateIfExists[T comparable](changedFields *changes.Changes, key string, from *T, to *T) bool {
	if to == nil || from == nil || *to == *from {
		return false
	}

	*changedFields = append(*changedFields, changes.Change{
		Key:  key,
		From: fmt.Sprintf("%v", *from),
		To:   fmt.Sprintf("%v", *to),
	})
	*from = *to
	return true
}

// SanitizeAndUpdateIfExists sanitizes to with utils.SanitizeString and, when it differs from from, validates it
// with the validators, sets from to it and adds the change of key to changedFields. It returns the first
// validation error, nothing is changed then. It does nothing when to or from is nil.
func SanitizeAndUpdateIfExists(changedFields *Changes, key string, from *string, to *string, validators ...func(string) error) error {
	if to == nil || from == nil {
		return nil
	}

	*to = utils.SanitizeString(*to)
	if *to == *from {
		return nil
	}

	for _, validate := range validators {
		if err := validate(*to); err != nil {
			return err
		}
	}

	*changedFields = append(*changedFields, changes.Change{
		Key:  key,
		From: *from,
		To:   *to,
	})
	*from = *to
	return nil
}

// SetIfExists sets dst to the value of src when src is not nil, it reports whether dst was set.
func SetIfExists[T any](dst *T, src *T) bool {
	if src == nil {
		return false
	}
	*dst = *src
	return true
}

// CreateLogInfo stores an info log of the action with the changes, by the user of the request.
func CreateLogInfo(ctx context.Context, action string, changes changes.Changes) error {
	return logger.Info(action).WithChanges(changes).WithUser(GetUser(ctx)).Create()
}

// CreateLogError stores an error log of the action with the changes and the error, by the user of the request.
func CreateLogError(ctx context.Context, action string, changes changes.Changes, err error) error {
	return logger.Error(action).WithMessage(err.Error()).WithChanges(changes).WithUser(GetUser(ctx)).Create()
}

// PointerSliceToSlice returns the values of the pointers in src. The pointers must not be nil.
func PointerSliceToSlice[T any](src []*T) []T {
	dst := make([]T, len(src))
	for i, v := range src {
		dst[i] = *v
	}
	return dst
}

// SliceToPointerSlice returns pointers to copies of the values in src.
func SliceToPointerSlice[T any](src []T) []*T {
	dst := make([]*T, len(src))
	for i, v := range src {
		dst[i] = &v
	}
	return dst
}

// Point returns a pointer to a copy of v.
func Point[T any](v T) *T {
	return &v
}

// Stringify formats e as a change value, see changes.Stringify.
func Stringify(e any) string {
	return changes.Stringify(e)
}
