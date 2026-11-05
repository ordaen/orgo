package model

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUIDJSON(t *testing.T) {
	b, err := json.Marshal(UUID(`a"b`))
	require.NoError(t, err)
	assert.Equal(t, `"a\"b"`, string(b))

	for in, want := range map[string]UUID{
		`"123e4567-e89b-12d3-a456-426614174000"`: "123e4567-e89b-12d3-a456-426614174000",
		`"a\"b"`:                                 `a"b`,
		`"A"`:                                    "A",
	} {
		var id UUID
		require.NoError(t, json.Unmarshal([]byte(in), &id), in)
		assert.Equal(t, want, id, in)
	}

	// null and an empty string leave the UUID unchanged
	for _, in := range []string{`null`, `""`} {
		id := UUID("kept")
		require.NoError(t, json.Unmarshal([]byte(in), &id), in)
		assert.Equal(t, UUID("kept"), id, in)
	}

	var id UUID
	assert.Error(t, json.Unmarshal([]byte(`123`), &id))
}

func TestSID(t *testing.T) {
	var _ ModelID = SID("")
	assert.False(t, SID("").Valid())
	assert.True(t, SID("default").Valid())
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb924", SID("e3b0c44298fc1c149afbf4c8996fb924").String())

	type rec struct {
		ID  SID `json:"id"`
		Opt SID `json:"opt,omitempty"`
	}
	b, err := json.Marshal(rec{ID: `a"b`})
	require.NoError(t, err)
	assert.Equal(t, `{"id":"a\"b"}`, string(b))

	r := rec{ID: "kept"}
	require.NoError(t, json.Unmarshal([]byte(`{"id":null,"opt":"m1.small"}`), &r))
	assert.Equal(t, rec{ID: "kept", Opt: "m1.small"}, r)
}

func TestUUIDGQL(t *testing.T) {
	var buf bytes.Buffer
	UUID("abc").MarshalGQL(&buf)
	assert.Equal(t, `"abc"`, buf.String())

	var id UUID
	require.NoError(t, id.UnmarshalGQL("abc"))
	assert.Equal(t, UUID("abc"), id)
	assert.Error(t, id.UnmarshalGQL(1))
}

type sidModel struct {
	Base[ID]
	OpenstackID string
}

func (*sidModel) TableName() string { return "sid_models" }
func (m *sidModel) GetSID() SID     { return SID(m.OpenstackID) }

func TestOwnerID(t *testing.T) {
	assert.Equal(t, "7", OwnerID(&sidModel{Base: Base[ID]{ID: 7}}), "the ID without a SID")
	assert.Equal(t, "e3b0c442", OwnerID(&sidModel{Base: Base[ID]{ID: 7}, OpenstackID: "e3b0c442"}))
	assert.Equal(t, "8", OwnerID(&testModelWithUUID{Base: Base[UUID]{ID: "8"}}), "a model without GetSID")
}
