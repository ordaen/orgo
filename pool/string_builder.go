package pool

import (
	"strings"
	"sync"
)

var StringBuilder = &stringBuilder{
	pool: sync.Pool{
		New: func() any { return new(strings.Builder) },
	},
}

type stringBuilder struct {
	pool sync.Pool
}

// Get returns an empty builder
func (p *stringBuilder) Get() *strings.Builder {
	buf, ok := p.pool.Get().(*strings.Builder)
	if ok {
		return buf
	}
	return new(strings.Builder)
}

// Put resets the builder and puts it back to the pool, it must not be used after
func (p *stringBuilder) Put(buf *strings.Builder) {
	if buf == nil || buf.Cap() > MaxSize {
		return
	}
	buf.Reset()
	p.pool.Put(buf)
}
