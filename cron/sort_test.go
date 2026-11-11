package cron

import (
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The default order of the lists, see gql.Querier, is stable: logs created at the same time, like in one
// transaction, and records with the same name come one page after the other without overlap.
func TestSortByPagesDoNotOverlap(t *testing.T) {
	clearTables(t)
	var logIDs, recordIDs []model.ID
	for range 3 {
		l, err := Logs.Create(&CronLog{Handler: "h", Status: "complete"})
		require.NoError(t, err)
		logIDs = append([]model.ID{l.ID}, logIDs...) // the last first
		r, err := Records.Create(&CronRecord{Handler: "h", Name: "same"})
		require.NoError(t, err)
		recordIDs = append(recordIDs, r.ID)
	}
	_, err := pg.Exec("UPDATE cron_logs SET created = '2026-01-01 00:00:00+00'")
	require.NoError(t, err)

	assert.Equal(t, logIDs, pageIDs(t, Logs.Query, (&CronLog{}).SortBy()), "the logs of the same time by id, the last first")
	assert.Equal(t, recordIDs, pageIDs(t, Records.Query, (&CronRecord{}).SortBy()), "the records of the same name by id")
}

// pageIDs returns the ids of the records read one per page in the order.
func pageIDs[T model.Model](t *testing.T, query func() *pg.ModelQuery[T], order string) []model.ID {
	t.Helper()
	var ids []model.ID
	for offset := 0; ; offset++ {
		recs, err := query().Order(order).Limit(1).Offset(offset).Select()
		require.NoError(t, err)
		if len(recs) == 0 {
			return ids
		}
		ids = append(ids, recs[0].GetID().(model.ID))
	}
}
