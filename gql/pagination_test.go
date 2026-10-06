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
