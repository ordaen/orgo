package repo

import "sync"

// writeTracker orders the cache changes of a Cached repository with its database reads and writes, so a record read
// from the database before a write is not cached after the write changed the cache.
//
// Every operation begins by taking the current write generation. Every write increments it and records it as the
// last write of its key. A record read by an operation is cached only when its key was not written since the
// operation began. The last writes are kept only while operations are in flight, so the tracker does not grow.
type writeTracker struct {
	mu       sync.Mutex
	gen      uint64
	written  map[string]uint64 // key -> generation of its last write
	inflight int
	// afterDB is called by tests after the database query of an operation, before the cache is changed.
	afterDB func()
}

func newWriteTracker() *writeTracker {
	return &writeTracker{written: make(map[string]uint64)}
}

// begin starts an operation and returns the generation it began at. The operation must be ended with end.
func (w *writeTracker) begin() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.inflight++
	return w.gen
}

// end ends an operation started by begin.
func (w *writeTracker) end() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.inflight--
	if w.inflight == 0 {
		clear(w.written)
	}
}

// queried calls the afterDB test hook.
func (w *writeTracker) queried() {
	if w.afterDB != nil {
		w.afterDB()
	}
}

// changedSince reports whether the key was written since the generation. w.mu must be held.
func (w *writeTracker) changedSince(key string, since uint64) bool {
	return w.written[key] > since
}

// fill calls set to cache a record read by an operation begun at since, unless its key was written since then.
func (w *writeTracker) fill(key string, since uint64, set func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.changedSince(key, since) {
		set()
	}
}

// write records a write of the key by an operation begun at since and calls set to cache its result.
// When the key was written by another operation since then, the order of the two writes is not known,
// so it calls del instead and returns false: the record must be read again to be cached.
func (w *writeTracker) write(key string, since uint64, set, del func()) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	conflict := w.changedSince(key, since)
	w.gen++
	w.written[key] = w.gen
	if conflict {
		del()
		return false
	}
	set()
	return true
}

// locked calls fn with w.mu held, for the cache changes of an operation begun at since that span several keys.
// changed reports whether a key was written since then, keys are the keys written since then.
func (w *writeTracker) locked(since uint64, fn func(changed func(key string) bool, keys []string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var keys []string
	for key, gen := range w.written {
		if gen > since {
			keys = append(keys, key)
		}
	}
	fn(func(key string) bool { return w.changedSince(key, since) }, keys)
}
