//go:build darwin

package transport

import (
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"
)

// playbackStateNotification is the distributed notification Spotify
// posts when it starts, pauses, resumes, seeks or changes track.
const playbackStateNotification = "com.spotify.client.PlaybackStateChanged"

// source is the Source of a macOS build. It listens with
// NSDistributedNotificationCenter, which needs no cgo.
type source struct {
	mu       sync.Mutex
	center   objc.ID
	observer objc.ID
	block    objc.Block
}

// NewSource returns a Source that listens for Spotify's playback
// notifications. Start it, then run RunLoop on the main thread.
func NewSource() Source {
	return &source{}
}

// Start registers for Spotify's playback notifications. The handler is
// called on the main thread, inside the run loop that RunLoop runs.
func (s *source) Start(handler func()) error {
	if handler == nil {
		return errors.New("transport: no change handler")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observer != 0 {
		return errors.New("transport: source already started")
	}

	centerClass := objc.GetClass("NSDistributedNotificationCenter")
	if centerClass == 0 {
		return errors.New("transport: the notification center is unavailable")
	}

	var center, observer objc.ID
	var block objc.Block
	withPool(func() {
		name := nsString(playbackStateNotification)
		center = objc.ID(centerClass).Send(sel("defaultCenter"))
		if center == 0 {
			return
		}
		// The block runs later, on the main thread, so the center copies
		// it; the copy here is the one this source owns and releases in
		// Close.
		block = objc.NewBlock(func(objc.Block, objc.ID) {
			handler()
		})
		observer = center.Send(sel("addObserverForName:object:queue:usingBlock:"),
			name, objc.ID(0), objc.ID(0), block)
	})

	if center == 0 {
		return errors.New("transport: could not reach the notification center")
	}
	if observer == 0 {
		if block != 0 {
			withPool(func() { block.Release() })
		}
		return errors.New("transport: could not register for Spotify notifications")
	}

	s.center = center
	s.observer = observer
	s.block = block
	return nil
}

// Close stops listening. The run loop keeps running; there is nothing
// else it would do.
func (s *source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observer == 0 {
		return nil
	}

	withPool(func() {
		s.center.Send(sel("removeObserver:"), s.observer)
		s.block.Release()
	})
	s.center = 0
	s.observer = 0
	s.block = 0
	return nil
}

// RunLoop runs the main thread's run loop, where macOS delivers
// distributed notifications, and never returns.
//
// Call it directly from main, never from a goroutine and never with "go":
//
//	go sdk.Run(...)
//	transport.RunLoop()
//
// RunLoop pins the goroutine to the thread it is already on, and macOS
// runs a run loop only on the process's main thread. A "go
// transport.RunLoop()" would pin some other thread and never receive a
// notification.
func RunLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	loop := objc.ID(objc.GetClass("NSRunLoop")).Send(sel("mainRunLoop"))
	for {
		withPool(func() {
			loop.Send(sel("runUntilDate:"), nsDate(1*time.Second))
		})
	}
}

// sel is objc.RegisterName under a shorter name, for the selectors this
// source sends.
func sel(name string) objc.SEL {
	return objc.RegisterName(name)
}

// nsString returns a NSString for a Go string, which the caller releases
// with its pool.
func nsString(value string) objc.ID {
	// stringWithUTF8String: copies the Go string's bytes, so the
	// trailing NUL is all it needs to find the end.
	return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), value+"\x00")
}

// nsDate returns an NSDate the given time from now, for one turn of the
// run loop.
func nsDate(after time.Duration) objc.ID {
	return objc.ID(objc.GetClass("NSDate")).Send(sel("dateWithTimeIntervalSinceNow:"), after.Seconds())
}

// withPool runs fn inside an autorelease pool, so the objects the
// framework returns find an owner. A pool belongs to its thread, so fn
// stays on one until the pool drains.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("alloc")).Send(sel("init"))
	defer func() {
		if pool != 0 {
			pool.Send(sel("drain"))
		}
	}()
	fn()
}
