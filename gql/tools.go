package gql

import (
	"context"
	"fmt"

	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/utils"

	"github.com/ordaen/orgo/changes"
)

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

func SetIfExists[T any](dst *T, src *T) bool {
	if src == nil {
		return false
	}
	*dst = *src
	return true
}

func CreateLogInfo(ctx context.Context, action string, changes changes.Changes) error {
	return logger.Info(action).WithChanges(changes).WithUser(GetUser(ctx)).Create()
}

func CreateLogError(ctx context.Context, action string, changes changes.Changes, err error) error {
	return logger.Error(action).WithMessage(err.Error()).WithChanges(changes).WithUser(GetUser(ctx)).Create()
}

func PointerSliceToSlice[T any](src []*T) []T {
	dst := make([]T, len(src))
	for i, v := range src {
		dst[i] = *v
	}
	return dst
}

func SliceToPointerSlice[T any](src []T) []*T {
	dst := make([]*T, len(src))
	for i, v := range src {
		dst[i] = &v
	}
	return dst
}

func Point[T any](v T) *T {
	return &v
}

func Stringify(e any) string {
	return changes.Stringify(e)
}
