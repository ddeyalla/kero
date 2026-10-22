package spotify

import (
	"context"
	"errors"
	"sync"
	"time"

	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/builtin/spotify/transport"
)

// engineIdleTimeout is how long the engine's web view stays open with
// nothing playing. A hidden page that sits paused for a long time is frozen
// by the system, and its web view costs memory all along, so it goes away
// and the next play opens it again. A test shortens it.
var engineIdleTimeout = 10 * time.Minute

// engineWakeTimeout is how long a play waits for a web view that was just
// opened to report ready. The play is sent to the page only then, so that
// the time of the cold start is not counted as the time the play waited
// for the page, which would drop it as stale.
var engineWakeTimeout = 20 * time.Second

// engineWakePoll is how often a play that waits for the web view looks
// whether it is ready.
var engineWakePoll = 20 * time.Millisecond

// engineEager makes the plugin open the web view when it makes the engine,
// as it did before the engine was lazy. A test that reads the engine without
// a play sets it.
var engineEager = false

// errNoEngine is the answer of a play when the host gave no web view to run
// the engine in.
var errNoEngine = errors.New("spotify: no playback engine")

// idler is a player whose web view the plugin can let go of: lazyEngine.
type idler interface {
	// Acquire says a pane is open, and Release that the last one closed.
	Acquire()
	Release()
}

// lazyEngine is the engine that opens its web view only when a play wants
// it, and closes it again after engineIdleTimeout with nothing playing, or
// when the last pane closes while nothing plays. Until then it is a player
// with nothing to play: it reads as stopped, and the commands that need a
// playing track do nothing. It is the plugin's engine, so the panes show the
// player and not the sign-in while the web view is closed.
type lazyEngine struct {
	// build returns a new engine for the given hooks, nil when the host has
	// no web view to give.
	build func(emit func(transport.Snapshot), onError func(error)) playerEngine
	// emit and onError are the plugin's hooks, and idled tells the plugin
	// that the web view went away on its own.
	emit    func(transport.Snapshot)
	onError func(error)
	idled   func()
	// wakeTimeout is how long a play waits for the web view it opened; it
	// is engineWakeTimeout when the engine is made. A field, so that a play
	// still waiting in a test that ended does not read what the next test
	// sets.
	wakeTimeout time.Duration

	// wakeMu lets one play open the web view while the others wait.
	wakeMu sync.Mutex

	mu    sync.Mutex
	inner playerEngine
	// gen numbers the engines, so that a hook or a timer of an engine that
	// is gone does not act on the next one.
	gen     int
	closed  bool
	playing bool
	// released says no pane is open.
	released bool
	// volume is the last volume the engine reported, kept while the web
	// view is closed.
	volume int
	timer  *time.Timer
}

// newLazyEngine returns an engine with no web view yet.
func newLazyEngine(
	build func(emit func(transport.Snapshot), onError func(error)) playerEngine,
	emit func(transport.Snapshot), onError func(error), idled func(),
) *lazyEngine {
	return &lazyEngine{build: build, emit: emit, onError: onError, idled: idled, wakeTimeout: engineWakeTimeout}
}

// Start does nothing: the web view opens at the first play.
func (w *lazyEngine) Start() error { return nil }

// Close ends the web view, if there is one, and the engine for good.
func (w *lazyEngine) Close() error {
	w.mu.Lock()
	w.closed = true
	inner := w.inner
	w.inner = nil
	w.gen++
	w.stopIdleLocked()
	w.mu.Unlock()
	if inner != nil {
		return inner.Close()
	}
	return nil
}

// Acquire says a pane is open.
func (w *lazyEngine) Acquire() {
	w.mu.Lock()
	w.released = false
	w.mu.Unlock()
}

// Release says the last pane closed: a web view with nothing playing goes
// away at once, and one that plays goes when it stops.
func (w *lazyEngine) Release() {
	w.mu.Lock()
	w.released = true
	idle := w.inner != nil && !w.playing
	w.mu.Unlock()
	if idle {
		w.teardown(0)
	}
}

// wake opens the web view when it is closed, and waits for it to be ready.
// A web view that does not get ready in time is returned all the same: the
// command then fails with the engine's own answer.
func (w *lazyEngine) wake() (playerEngine, error) {
	w.wakeMu.Lock()
	defer w.wakeMu.Unlock()
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, engine.ErrClosed
	}
	inner := w.inner
	if inner == nil {
		w.gen++
		gen := w.gen
		inner = w.build(
			func(snap transport.Snapshot) { w.pushed(gen, snap) },
			func(err error) {
				w.mu.Lock()
				current := w.gen == gen
				w.mu.Unlock()
				if current {
					w.onError(err)
				}
			},
		)
		if inner == nil {
			w.mu.Unlock()
			return nil, errNoEngine
		}
		w.inner = inner
	}
	if !w.playing {
		// The web view is wanted now: a play that does not land still
		// leaves it a whole idle period.
		w.resetIdleLocked()
	}
	gen := w.gen
	w.mu.Unlock()

	// Start does nothing for an engine that runs already.
	if err := inner.Start(); err != nil {
		w.mu.Lock()
		if w.gen == gen && w.inner == inner {
			w.inner = nil
			w.gen++
			w.stopIdleLocked()
		}
		w.mu.Unlock()
		return nil, err
	}
	deadline := time.Now().Add(w.wakeTimeout)
	for {
		_, err := inner.Read(context.Background())
		if err == nil || time.Now().After(deadline) {
			return inner, nil
		}
		if !errors.Is(err, engine.ErrStarting) {
			return nil, err
		}
		time.Sleep(engineWakePoll)
	}
}

// pushed takes a state of the engine numbered gen, keeps what the idle
// timer needs of it, and passes it on.
func (w *lazyEngine) pushed(gen int, snap transport.Snapshot) {
	w.mu.Lock()
	if w.gen != gen || w.closed {
		w.mu.Unlock()
		return
	}
	w.volume = snap.Volume
	w.playing = snap.State == transport.StatePlaying
	release := false
	if w.playing {
		w.stopIdleLocked()
	} else if w.released {
		release = true
	} else if w.timer == nil {
		w.resetIdleLocked()
	}
	w.mu.Unlock()
	w.emit(snap)
	if release {
		// The pane that kept the web view open is gone, and the track
		// that kept it open stopped.
		go w.teardown(gen)
	}
}

// resetIdleLocked starts the idle period over. It is called with w.mu held.
func (w *lazyEngine) resetIdleLocked() {
	w.stopIdleLocked()
	gen := w.gen
	w.timer = time.AfterFunc(engineIdleTimeout, func() { w.teardown(gen) })
}

// stopIdleLocked ends the idle period. It is called with w.mu held.
func (w *lazyEngine) stopIdleLocked() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

// teardown closes the web view of the engine numbered gen, unless a track
// plays or the engine is another one now. Gen 0 means the current one.
func (w *lazyEngine) teardown(gen int) {
	w.mu.Lock()
	if w.inner == nil || w.closed || w.playing || (gen != 0 && w.gen != gen) {
		w.mu.Unlock()
		return
	}
	inner := w.inner
	w.inner = nil
	w.gen++
	w.stopIdleLocked()
	panes := !w.released
	w.mu.Unlock()
	_ = inner.Close()
	if panes {
		w.idled()
	}
}

// current is the engine with a web view, nil when it is closed.
func (w *lazyEngine) current() playerEngine {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.inner
}

// PlayPause flips between playing and paused, and opens the web view for it.
func (w *lazyEngine) PlayPause(ctx context.Context) error {
	inner, err := w.wake()
	if err != nil {
		return err
	}
	return inner.PlayPause(ctx)
}

// Play starts a track, and opens the web view for it.
func (w *lazyEngine) Play(ctx context.Context, uri string) error {
	inner, err := w.wake()
	if err != nil {
		return err
	}
	return inner.Play(ctx, uri)
}

// PlayFrom starts a track inside its album or playlist, and opens the web
// view for it.
func (w *lazyEngine) PlayFrom(ctx context.Context, contextURI, trackURI string) error {
	inner, err := w.wake()
	if err != nil {
		return err
	}
	if cp, ok := inner.(contextPlayer); ok {
		return cp.PlayFrom(ctx, contextURI, trackURI)
	}
	return inner.Play(ctx, trackURI)
}

// Next skips to the next track. With no web view nothing plays here.
func (w *lazyEngine) Next(ctx context.Context) error {
	if inner := w.current(); inner != nil {
		return inner.Next(ctx)
	}
	return nil
}

// Previous goes to the previous track.
func (w *lazyEngine) Previous(ctx context.Context) error {
	if inner := w.current(); inner != nil {
		return inner.Previous(ctx)
	}
	return nil
}

// Launch has no app to open: the engine is the player.
func (w *lazyEngine) Launch(context.Context) error { return transport.ErrNotInstalled }

// Seek moves to a position in the track.
func (w *lazyEngine) Seek(ctx context.Context, position time.Duration) error {
	if inner := w.current(); inner != nil {
		return inner.Seek(ctx, position)
	}
	return nil
}

// Volume sets the volume.
func (w *lazyEngine) Volume(ctx context.Context, volume int) error {
	if inner := w.current(); inner != nil {
		return inner.Volume(ctx, volume)
	}
	return nil
}

// Shuffle turns shuffle on or off.
func (w *lazyEngine) Shuffle(ctx context.Context, on bool) error {
	if inner := w.current(); inner != nil {
		return inner.Shuffle(ctx, on)
	}
	return nil
}

// Repeat turns repeat of the context on or off.
func (w *lazyEngine) Repeat(ctx context.Context, on bool) error {
	if inner := w.current(); inner != nil {
		return inner.Repeat(ctx, on)
	}
	return nil
}

// Read gives the state of the engine, or a stopped player while the web
// view is closed.
func (w *lazyEngine) Read(ctx context.Context) (transport.Snapshot, error) {
	w.mu.Lock()
	inner, volume := w.inner, w.volume
	w.mu.Unlock()
	if inner != nil {
		return inner.Read(ctx)
	}
	return transport.Snapshot{State: transport.StateStopped, Volume: volume, ReadAt: time.Now()}, nil
}

var _ playerEngine = (*lazyEngine)(nil)
var _ idler = (*lazyEngine)(nil)
var _ contextPlayer = (*lazyEngine)(nil)
