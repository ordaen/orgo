package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testModelWithID struct {
	Base[ID]
}

func (m *testModelWithID) TableName() string {
	return "test_models_with_id"
}

type testModelWithUUID struct {
	Base[UUID]
}

func (m *testModelWithUUID) TableName() string {
	return "test_models_with_uuid"
}

func TestBaseWithID(t *testing.T) {
	model := testModelWithID{
		ID: 1,
	}

	assert.Equal(t, ID(1), model.GetID())
	require.Equal(t, "test_models_with_id", model.TableName())
}

func TestBaseWithUUID(t *testing.T) {
	model := testModelWithUUID{
		ID: "123e4567-e89b-12d3-a456-426614174000",
	}

	assert.Equal(t, UUID("123e4567-e89b-12d3-a456-426614174000"), model.GetID())
	require.Equal(t, "test_models_with_uuid", model.TableName())
}
