package statuser

import (
	"testing"

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
