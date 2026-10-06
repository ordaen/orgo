// Package pool reuses bytes.Buffer and strings.Builder values, the ones that grew too large are not reused.
package pool

import (
	"bytes"
	"sync"
)

// MaxSize is the capacity above which a buffer or builder is dropped instead of being reused,
// so a rare large value does not keep its memory in the pool.
const MaxSize = 64 << 10

var Bytes = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Get returns an empty buffer
func Get() *bytes.Buffer {
	buf, ok := Bytes.Get().(*bytes.Buffer)
	if ok {
		return buf
	}
	return new(bytes.Buffer)
}

// Put resets the buffer and puts it back to the pool, it must not be used after
func Put(buf *bytes.Buffer) {
	if buf == nil || buf.Cap() > MaxSize {
		return
	}
	buf.Reset()
	Bytes.Put(buf)
}
