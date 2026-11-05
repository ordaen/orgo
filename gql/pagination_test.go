package gql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPagination(t *testing.T) {
	testData := []struct {
		page           *int
		limit          *int
		expectedPage   int
		expectedLimit  int
		expectedOffset int
	}{
		{nil, nil, DefaultPage, DefaultLimit, 0},
		{new(-1), new(-1), DefaultPage, DefaultLimit, 0},
		{new(10), new(25), 10, 25, (10 - 1) * 25},
		{new(10), new(MaxLimit + 1), 10, MaxLimit, (10 - 1) * MaxLimit},
		{new(10), new(0), 10, DefaultLimit, (10 - 1) * DefaultLimit},
	}
	for idx, test := range testData {
		pagination := NewPagination(test.page, test.limit)
		assert.Equal(t, test.expectedPage, pagination.Page, "index: %d", idx)
		assert.Equal(t, test.expectedLimit, pagination.Limit, "index: %d", idx)
		assert.Equal(t, test.expectedOffset, pagination.Offset(), "index: %d", idx)
	}
}

func TestSetMaxLimit(t *testing.T) {
	t.Cleanup(func() { SetMaxLimit(MaxLimit) })

	SetMaxLimit(1000)
	assert.Equal(t, 1000, NewPagination(nil, new(1000)).Limit)
	assert.Equal(t, 1000, NewPagination(nil, new(5000)).Limit)
	assert.Equal(t, DefaultLimit, NewPagination(nil, nil).Limit)

	// a limit that is not positive resets it to MaxLimit
	SetMaxLimit(0)
	assert.Equal(t, MaxLimit, NewPagination(nil, new(1000)).Limit)
	SetMaxLimit(-1)
	assert.Equal(t, MaxLimit, NewPagination(nil, new(1000)).Limit)
}
