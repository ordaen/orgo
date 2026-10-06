package pool

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBytes(t *testing.T) {
	buf := Get()
	buf.WriteString("data")
	Put(buf)
	assert.Zero(t, Get().Len(), "a buffer from the pool is empty")
	Put(nil)

	large := bytes.NewBuffer(make([]byte, 0, MaxSize+1))
	Put(large)
	for range 10 {
		assert.NotSame(t, large, Get(), "a large buffer is not reused")
	}
}

func TestStringBuilder(t *testing.T) {
	b := StringBuilder.Get()
	b.WriteString("data")
	StringBuilder.Put(b)
	assert.Zero(t, StringBuilder.Get().Len())
	StringBuilder.Put(nil)

	large := new(strings.Builder)
	large.Grow(MaxSize + 1)
	StringBuilder.Put(large)
	for range 10 {
		assert.NotSame(t, large, StringBuilder.Get())
	}
}
