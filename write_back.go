package main

import (
	"reflect"
	"sync"
	"time"
)

// writeDelay is how long a change waits for the next one before it is
// written: a session switch, or a drag of the sidebar, makes many.
const writeDelay = 500 * time.Millisecond

// writeBack writes a value to disk off the main thread. Set keeps the newest
// value, and one goroutine writes it once the delay is over, so a burst of
// changes costs one write; a value that is what the disk holds is not
// written again. Flush writes a value that waits before it returns, which
// quitting does.
type writeBack[T any] struct {
	delay time.Duration
	save  func(T) error

	mu    sync.Mutex
	value T
	last  T             // the value the disk holds
	known bool          // last is a value, as the file was read, or as one was written
	dirty bool          // a value waits for its write
	busy  bool          // the goroutine runs: it waits out the delay, or writes
	wrote int           // values written, for tests
	idle  chan struct{} // closed when the goroutine has written all it had
	wake  chan struct{} // closed to write before the delay is over
}

// newWriteBack is a writer of save, which is called on its own goroutine.
func newWriteBack[T any](delay time.Duration, save func(T) error) *writeBack[T] {
	return &writeBack[T]{delay: delay, save: save}
}

// Seed says that v is what the disk holds, as the file was read: saving it
// writes nothing.
func (w *writeBack[T]) Seed(v T) {
	w.mu.Lock()
	w.last, w.known = v, true
	w.mu.Unlock()
}

// Forget says that the writer does not know what the disk holds: the next
// value is written even when it is the one seeded, as the file it was read
// from is gone.
func (w *writeBack[T]) Forget() {
	w.mu.Lock()
	var zero T
	w.last, w.known = zero, false
	w.mu.Unlock()
}

// Set makes v the value to write. It returns at once: the write, and its
// delay, are the goroutine's.
func (w *writeBack[T]) Set(v T) {
	w.mu.Lock()
	w.value, w.dirty = v, true
	if w.busy {
		w.mu.Unlock()
		return
	}
	w.busy, w.idle, w.wake = true, make(chan struct{}), make(chan struct{})
	idle, wake := w.idle, w.wake
	w.mu.Unlock()
	go w.batch(idle, wake)
}

// batch writes the newest value once the delay is over, and any that came
// while it wrote, then says that it is idle.
func (w *writeBack[T]) batch(idle, wake chan struct{}) {
	for {
		select {
		case <-time.After(w.delay):
		case <-wake:
		}
		w.mu.Lock()
		v := w.value
		w.dirty = false
		same := w.known && reflect.DeepEqual(w.last, v)
		w.mu.Unlock()
		if !same {
			if err := w.save(v); err == nil {
				w.mu.Lock()
				w.last, w.known, w.wrote = v, true, w.wrote+1
				w.mu.Unlock()
			}
		}
		w.mu.Lock()
		waiting := w.dirty
		if !waiting {
			w.busy = false
			close(idle)
		}
		w.mu.Unlock()
		if !waiting {
			return
		}
	}
}

// Flush writes a value that waits before it returns, and the one after it,
// if one came meanwhile. A value still in its delay goes at once.
func (w *writeBack[T]) Flush() {
	w.mu.Lock()
	if !w.busy {
		w.mu.Unlock()
		return
	}
	select {
	case <-w.wake: // already woken
	default:
		close(w.wake)
	}
	idle := w.idle
	w.mu.Unlock()
	<-idle
}

// writes is how many values the writer wrote: a test counts them.
func (w *writeBack[T]) writes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wrote
}
