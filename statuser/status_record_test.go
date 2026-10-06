package statuser

import (
	"context"
	"testing"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelStatusLog(t *testing.T) {
	require.NoError(t, pg.ClearTables("status_records"))
	r := &StatusRecord{Status: "test", OwnerID: "1", OwnerType: "some_table"}
	assert.Equal(t, "status_records", r.TableName())
	assert.Equal(t, "StatusRecord", pg.ModelType(r))
	_, err := StatusRecords.Create(r)
	require.NoError(t, err)
}

func TestStatusRecordsFindByOwner(t *testing.T) {
	require.NoError(t, pg.ClearTables("status_records"))
	o := &order{ID: 5}
	doc := &document{ID: "6f1c2a52-6a45-4a6b-9b55-2b1a3f7f0c11"}
	user := &types.User{ID: model.ID(1), Name: "admin"}
	require.NoError(t, CreateStatusRecord(o, "new", "created", user))
	require.NoError(t, CreateStatusRecord(o, "paid", "", nil))
	require.NoError(t, CreateStatusRecord(doc, "draft", "", nil))

	recs := StatusRecords.FindByOwner(o)
	require.Len(t, recs, 2)
	assert.Equal(t, "new", recs[0].Status)
	assert.Equal(t, user, recs[0].Issuer)
	assert.Equal(t, ShortStatusRecord{Status: "new", Reason: "created", Time: recs[0].Created, User: user}, recs[0].Short())
	assert.Equal(t, "paid", recs[1].Status)
	assert.Nil(t, recs[1].Issuer)

	recs = StatusRecords.FindByOwner(doc)
	require.Len(t, recs, 1, "the owners with UUIDs are recorded")
	assert.Equal(t, string(doc.ID), recs[0].OwnerID)
}

// TestStatusRecordsFindByOwnerOrder checks the records are returned the oldest first, by id when they were created
// at the same time, like the changes of one transaction.
func TestStatusRecordsFindByOwnerOrder(t *testing.T) {
	require.NoError(t, pg.ClearTables("status_records"))
	o := &order{ID: 5}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// the records are inserted in another order than they were created, paid and packed at the same time
	for _, rec := range []struct {
		status string
		at     time.Time
	}{
		{"shipped", created.Add(2 * time.Hour)},
		{"paid", created.Add(time.Hour)},
		{"new", created},
		{"packed", created.Add(time.Hour)},
	} {
		require.NoError(t, CreateStatusRecord(o, rec.status, "", nil))
		_, err := pg.DB.Exec(context.Background(), `UPDATE status_records SET created = $1 WHERE status = $2`, rec.at, rec.status)
		require.NoError(t, err)
	}
	// a record of another owner type with the same owner_id
	_, err := StatusRecords.Create(&StatusRecord{Status: "draft", OwnerID: "5", OwnerType: "document"})
	require.NoError(t, err)

	var statuses []string
	for _, rec := range StatusRecords.FindByOwner(o) {
		statuses = append(statuses, rec.Status)
	}
	assert.Equal(t, []string{"new", "paid", "packed", "shipped"}, statuses)
}
