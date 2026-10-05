package repo

import (
	"strconv"
	"testing"

	"github.com/ordaen/orgo/cache"
	"github.com/ordaen/orgo/model"
)

// benchCached returns a repository with n cached records, it does not use the database.
func benchCached(n int) *Cached[*baseModel] {
	mc := cache.NewMemory[*baseModel](0)
	for i := range n {
		mc.Set(strconv.Itoa(i+1), &baseModel{Base: model.Base[model.ID]{ID: model.ID(i + 1)}, Email: "user" + strconv.Itoa(i+1) + "@example.com"})
	}
	return NewCached(&baseModel{}, mc)
}

func BenchmarkFindCached(b *testing.B) {
	repo := benchCached(10000)
	b.Run("match", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = repo.FindCached(func(m *baseModel) bool { return m.Email == "user5000@example.com" })
		}
	})
	b.Run("none", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = repo.FindCached(func(m *baseModel) bool { return m.Email == "nobody@example.com" })
		}
	})
	b.Run("many", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = repo.FindManyCached(func(m *baseModel) bool { return m.ID%100 == 0 })
		}
	})
}
