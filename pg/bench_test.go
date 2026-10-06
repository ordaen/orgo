package pg

import (
	"context"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
)

func BenchmarkBuildInsert(b *testing.B) {
	m := &baseModel{Name: "john", Email: "john@example.com"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BuildInsert(m)
	}
}

func BenchmarkBuildUpdate(b *testing.B) {
	m := &baseModel{Base: model.Base[model.ID]{ID: 1}, Name: "john", Email: "john@example.com"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BuildUpdate(m)
	}
}

func BenchmarkBuildDelete(b *testing.B) {
	m := &baseModel{Base: model.Base[model.ID]{ID: 1}}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BuildDelete(m)
	}
}

func BenchmarkQueryBuild(b *testing.B) {
	q := Query(&builderOrderModel{}).Where("name = ?", "john").Order("id").Limit(10)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = q.build("*", true)
	}
}

// benchRow has fields that cannot be NULL and nillable ones.
type benchRow struct {
	ID      int64
	Name    string
	Active  bool
	At      time.Time
	Note    *string
	Score   float64
	Comment string // NULL in every other row
}

func BenchmarkMapRows(b *testing.B) {
	ctx := context.Background()
	const sql = `SELECT i AS id, 'name ' || i AS name, i % 2 = 0 AS active, now() AS at,
		CASE WHEN i % 2 = 0 THEN 'note' END AS note, i * 1.5 AS score,
		CASE WHEN i % 2 = 0 THEN 'comment' END AS comment
		FROM generate_series(1, 1000) AS i`
	b.ReportAllocs()
	for b.Loop() {
		rows, err := DB.Query(ctx, sql)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := MapRows[benchRow](rows); err != nil {
			b.Fatal(err)
		}
	}
}
