package spotify

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"

	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/protocol"
)

// asyncPlay presses a row and waits for its synchronous play to be accepted.
// It returns the request number the plugin gave that play.
func asyncPlay(t *testing.T, p *plugin, row entry) uint64 {
	t.Helper()
	p.playEntry("p1", row)
	eventually(t, "the play to be accepted", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playSeq
}

func taggedPlayError(req uint64, command, message string) error {
	return &engine.PlaybackError{Kind: "command", Command: command, Request: strconv.FormatUint(req, 10), Message: message}
}

var asyncRows = []entry{
	{id: "album-a1", title: "Album One", uri: "spotify:album:a1"},
	{id: "album-a2", title: "Album Two", uri: "spotify:album:a2"},
}

// A page error from an older play request must not revert the newer row,
// guess, mode or notice.
func TestAnOlderPagePlayErrorCannotOwnANewerPlay(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	a, b := asyncRows[0], asyncRows[1]
	first := asyncPlay(t, p, a)
	second := asyncPlay(t, p, b)

	p.mu.Lock()
	guess := s.guess
	if guess == nil || s.snap.Track != "Album Two" || p.playReq != second {
		p.mu.Unlock()
		t.Fatalf("before the old error: track=%q guess=%+v req=%d want %d", s.snap.Track, guess, p.playReq, second)
	}
	p.mu.Unlock()

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(first, "play", "Spotify answered 500 old A failed"))
	p.mu.Lock()
	if s.snap.Track != "Album Two" || s.guess == nil || s.playback != modePlaying || s.notice != "" || p.playReq != second {
		p.mu.Unlock()
		t.Fatalf("older page error changed the newer play: track=%q guess=%+v mode=%v notice=%q req=%d want %d",
			s.snap.Track, s.guess, s.playback, s.notice, p.playReq, second)
	}
	p.mu.Unlock()

	// The same request's error is current, so it reports and takes back its
	// own guess.
	p.playbackErrorFor(e, p.engineGen, taggedPlayError(second, "play", "Spotify answered 500 current failed"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" || s.guess != nil || p.playReq != 0 {
		t.Fatalf("current page error: notice=%q guess=%+v req=%d, want a notice and no guess", s.notice, s.guess, p.playReq)
	}
}

// A, B, then A again: the first A's page error must not own the latest A,
// even though the URI is the same.
func TestARepeatedPagePlayErrorNeedsTheLatestRequest(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	a, b := asyncRows[0], asyncRows[1]
	first := asyncPlay(t, p, a)
	asyncPlay(t, p, b)
	latest := asyncPlay(t, p, a)

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(first, "play", "old A failed"))
	p.mu.Lock()
	if s.snap.Track != "Album One" || s.guess == nil || s.notice != "" || p.playReq != latest {
		p.mu.Unlock()
		t.Fatalf("older first A changed the latest A: track=%q guess=%+v notice=%q req=%d want %d",
			s.snap.Track, s.guess, s.notice, p.playReq, latest)
	}
	p.mu.Unlock()

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(latest, "play", "latest A failed"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" || s.guess != nil || p.playReq != 0 {
		t.Fatalf("latest A failure: notice=%q guess=%+v req=%d", s.notice, s.guess, p.playReq)
	}
}

// coldWaitingEngine admits the first play and holds the second, like a web
// view that a newer play is waiting for.
type coldWaitingEngine struct {
	*fakeEngine
	mu      sync.Mutex
	calls   int
	started chan int
	ready   chan error
}

func (e *coldWaitingEngine) PlayRequest(ctx context.Context, uri, _ string) error {
	_ = e.record("play " + uri)
	e.mu.Lock()
	n := e.calls
	e.calls++
	e.mu.Unlock()
	e.started <- n
	if n == 0 {
		return nil
	}
	select {
	case err := <-e.ready:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A newer request that waits before its engine call already owns the
// selection: an older page error cannot take its row.
func TestANewerPlayWaitingForTheEngineOwnsTheSelection(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &coldWaitingEngine{fakeEngine: &fakeEngine{ready: true}, started: make(chan int, 2), ready: make(chan error, 1)}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	a, b := asyncRows[0], asyncRows[1]
	first := asyncPlay(t, p, a)
	<-e.started

	p.playEntry("p1", b)
	if got := <-e.started; got != 1 {
		t.Fatalf("second play started as %d", got)
	}
	p.mu.Lock()
	waiting := p.playReq
	if s.snap.Track != "Album Two" || s.guess == nil || waiting != first+1 {
		p.mu.Unlock()
		t.Fatalf("waiting play: track=%q guess=%+v req=%d want %d", s.snap.Track, s.guess, waiting, first+1)
	}
	p.mu.Unlock()

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(first, "play", "old A failed"))
	p.mu.Lock()
	if s.snap.Track != "Album Two" || s.guess == nil || s.notice != "" || p.playReq != waiting {
		p.mu.Unlock()
		t.Fatalf("old error took a waiting play: track=%q guess=%+v notice=%q req=%d want %d",
			s.snap.Track, s.guess, s.notice, p.playReq, waiting)
	}
	p.mu.Unlock()

	e.ready <- nil
	eventually(t, "the waiting play to finish", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
}

// A non-play command error reports, but does not take back a newer play's
// row or guess.
func TestANonPlayCommandErrorLeavesANewerPlayAlone(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	asyncPlay(t, p, asyncRows[0])
	latest := asyncPlay(t, p, asyncRows[1])

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(latest, "next", "the skip failed"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Album Two" || s.guess == nil || s.playback != modePlaying {
		t.Fatalf("a skip error took the play back: track=%q guess=%+v mode=%v", s.snap.Track, s.guess, s.playback)
	}
	if s.notice == "" {
		t.Fatal("a current non-play failure must still report")
	}
	if p.playReq != latest {
		t.Fatalf("a skip consumed the play request: req=%d want %d", p.playReq, latest)
	}
}

// Current Premium and authentication failures still report and take back
// the current play's guess.
func TestCurrentAccountAndSignInErrorsStillReport(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *engine.PlaybackError
	}{
		{"premium", &engine.PlaybackError{Kind: "account_error", Message: "PREMIUM_REQUIRED"}},
		{"sign-in", &engine.PlaybackError{Kind: "authentication_error", Message: "the token was refused"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useTimers(t)
			p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
			e := &fakeEngine{ready: true}
			p.engine, p.player = e, e
			s.snap.URI = "spotify:track:old"
			asyncPlay(t, p, asyncRows[0])

			p.playbackErrorFor(e, p.engineGen, tc.err)
			p.mu.Lock()
			defer p.mu.Unlock()
			if s.notice == "" || s.guess != nil {
				t.Fatalf("%s: notice=%q guess=%+v, want the cause and no guess", tc.name, s.notice, s.guess)
			}
		})
	}
}

// A retired inner engine's delayed error must not reach the plugin's state.
func TestAnErrorFromARetiredInnerEngineIsIgnored(t *testing.T) {
	var callbacks []func(error)
	var seen []error
	w := newLazyEngine(
		func(_ func(transport.Snapshot), onError func(error)) playerEngine {
			callbacks = append(callbacks, onError)
			return &fakeEngine{ready: true}
		},
		func(transport.Snapshot) {},
		func(err error) { seen = append(seen, err) },
		func() {},
	)
	if _, err := w.wake(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := callbacks[0]
	w.teardown(0)
	if _, err := w.wake(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(seen)
	old(errors.New("the old engine failed"))
	if len(seen) != before {
		t.Fatal("a retired inner engine's error reached the plugin")
	}
	callbacks[1](errors.New("the current engine failed"))
	if len(seen) != before+1 {
		t.Fatal("the current inner engine's error was not reported")
	}
}

// heldPlayEngine admits one play only when the test releases it.
type heldPlayEngine struct {
	*fakeEngine
	started chan uint64
	gate    chan error
	mu      sync.Mutex
	calls   int
}

func (e *heldPlayEngine) PlayRequest(ctx context.Context, uri, request string) error {
	_ = e.record("play " + uri)
	_ = request
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	e.started <- 1
	select {
	case err := <-e.gate:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A page error can reach the plugin before the synchronous Play call returns.
// The later success must not clear that current error's notice.
func TestAPageErrorBeforePlayReturnsKeepsItsNotice(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &heldPlayEngine{fakeEngine: &fakeEngine{ready: true}, started: make(chan uint64, 1), gate: make(chan error, 1)}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	p.playEntry("p1", asyncRows[0])
	<-e.started
	p.mu.Lock()
	req := p.playReq
	p.mu.Unlock()

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(req, "play", "current failed"))
	p.mu.Lock()
	if s.notice == "" || s.guess != nil {
		p.mu.Unlock()
		t.Fatalf("before Play returned: notice=%q guess=%+v", s.notice, s.guess)
	}
	p.mu.Unlock()

	e.gate <- nil
	eventually(t, "Play to return", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	time.Sleep(50 * time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" || s.guess != nil {
		t.Fatalf("the later success cleared the current error: notice=%q guess=%+v", s.notice, s.guess)
	}
}

// heldAdmissionEngine holds one play before the engine takes it, and stops
// when its caller does. A later play is accepted at once.
type heldAdmissionEngine struct {
	*fakeEngine
	entered  chan struct{}
	admitted chan string
	mu       sync.Mutex
	seen     []string
}

func (e *heldAdmissionEngine) PlayRequest(ctx context.Context, uri, request string) error {
	_ = request
	if uri == "spotify:album:a1" {
		close(e.entered)
		<-ctx.Done()
		return ctx.Err()
	}
	e.mu.Lock()
	e.seen = append(e.seen, uri)
	e.mu.Unlock()
	e.admitted <- uri
	return nil
}

func (e *heldAdmissionEngine) plays() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.seen...)
}

// A click that is still on its way to the engine is superseded by a newer
// click: the older one returns without being admitted, and the accepted
// order ends with the newer selection.
func TestANewerPlaySupersedesAnUnadmittedOlderPlay(t *testing.T) {
	useTimers(t)
	p, _, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &heldAdmissionEngine{fakeEngine: &fakeEngine{ready: true}, entered: make(chan struct{}), admitted: make(chan string, 2)}
	p.engine, p.player = e, e
	a, b := asyncRows[0], asyncRows[1]

	p.playEntry("p1", a)
	select {
	case <-e.entered:
	case <-time.After(time.Second):
		t.Fatal("the older play did not reach admission")
	}
	p.playEntry("p1", b)
	select {
	case uri := <-e.admitted:
		if uri != b.uri {
			t.Fatalf("admitted %q, want %q", uri, b.uri)
		}
	case <-time.After(time.Second):
		t.Fatal("the newer play was not admitted")
	}
	eventually(t, "the newer play to finish", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	if got := e.plays(); len(got) != 1 || got[0] != b.uri {
		t.Fatalf("admitted plays = %v, want only %q", got, b.uri)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playReq != 2 {
		t.Fatalf("current request = %d, want the newer one", p.playReq)
	}
}

// A coalescingEngine keeps the identity of the play it accepted and returns
// it for an identical click that it does not send again.
type coalescingEngine struct {
	*fakeEngine
	mu       sync.Mutex
	accepted string
}

func (e *coalescingEngine) PlayRequest(_ context.Context, _, request string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.accepted == "" {
		e.accepted = request
		return nil
	}
	return &engine.CoalescedError{Request: e.accepted}
}

// Two same-row clicks inside the engine's window are one accepted command.
// Its page error belongs to the current click too, rather than looking old.
func TestACoalescedClickKeepsTheAcceptedRequestAsOwner(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &coalescingEngine{fakeEngine: &fakeEngine{ready: true}}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	a := asyncRows[0]

	asyncPlay(t, p, a)
	asyncPlay(t, p, a)
	p.mu.Lock()
	owner := p.playReq
	if owner != 1 {
		p.mu.Unlock()
		t.Fatalf("owner = %d, want the accepted request 1", owner)
	}
	p.mu.Unlock()

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(owner, "play", "the accepted play failed"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" || s.guess != nil || p.playReq != 0 {
		t.Fatalf("the coalesced click's error: notice=%q guess=%+v req=%d", s.notice, s.guess, p.playReq)
	}
}

// heldCoalescingEngine accepts the first play, and holds the answer for a
// duplicate until the test releases it. That leaves the duplicate waiting
// for the accepted request while the accepted command's failure arrives.
type heldCoalescingEngine struct {
	*fakeEngine
	mu       sync.Mutex
	accepted string
	held     chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (e *heldCoalescingEngine) PlayRequest(ctx context.Context, uri, request string) error {
	e.mu.Lock()
	if e.accepted == "" {
		e.accepted = request
		e.mu.Unlock()
		return e.record("play " + uri)
	}
	accepted := e.accepted
	e.mu.Unlock()
	e.once.Do(func() { close(e.held) })
	select {
	case <-e.release:
		return &engine.CoalescedError{Request: accepted}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// The accepted command can fail before the engine has told a duplicate click
// which request it joined. The failure is kept and applied at the binding,
// rather than looking stale and leaving the sole accepted play silent.
func TestACoalescedFailureBeforeOwnerBindingIsKept(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &heldCoalescingEngine{
		fakeEngine: &fakeEngine{ready: true},
		held:       make(chan struct{}),
		release:    make(chan struct{}),
	}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	row := asyncRows[0]

	accepted := asyncPlay(t, p, row)
	p.playEntry("p1", row)
	select {
	case <-e.held:
	case <-time.After(time.Second):
		t.Fatal("the duplicate click did not reach the engine")
	}

	p.playbackErrorFor(e, p.engineGen, taggedPlayError(accepted, "play", "the accepted play failed"))
	p.mu.Lock()
	if s.notice != "" || p.playReq != accepted+1 {
		p.mu.Unlock()
		t.Fatalf("before binding: notice=%q req=%d, want no notice and the duplicate owner %d", s.notice, p.playReq, accepted+1)
	}
	p.mu.Unlock()

	close(e.release)
	eventually(t, "the duplicate click to bind", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" || s.guess != nil || p.playReq != 0 {
		t.Fatalf("after binding: notice=%q guess=%+v req=%d, want the accepted failure", s.notice, s.guess, p.playReq)
	}
	if calls := e.calls(); len(calls) != 1 {
		t.Fatalf("engine received %v, want only the accepted play", calls)
	}
}

// A deferred failure belongs to the click that was waiting when it arrived.
// Once that click finishes, the next click must not inherit its cache.
func TestADeferredFailureDoesNotOutliveItsPendingClick(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"

	for i := 0; i < 8; i++ {
		oldURI := "spotify:album:old" + strconv.Itoa(i)
		p.playEntry("p1", entry{title: "Old", uri: oldURI})
		eventually(t, "the old play to finish", func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return p.playing == ""
		})
		p.mu.Lock()
		old := p.playReq
		p.mu.Unlock()

		pendingURI := "spotify:album:pending" + strconv.Itoa(i)
		req, seq, _, cancel, ok := p.beginPlay("p1", entry{title: "Pending", uri: pendingURI}, pendingURI, "")
		if !ok {
			t.Fatal("the pending play was rejected")
		}
		p.playbackErrorFor(e, p.engineGen, taggedPlayError(old, "play", "an older accepted play failed"))
		p.mu.Lock()
		p.finishPlay(req, pendingURI, seq)(s, nil)
		p.mu.Unlock()
		cancel()
	}

	h := &heldCoalescingEngine{
		fakeEngine: &fakeEngine{ready: true},
		held:       make(chan struct{}),
		release:    make(chan struct{}),
	}
	p.engine, p.player = h, h
	row := entry{title: "Album A", uri: "spotify:album:a"}
	accepted := asyncPlay(t, p, row)
	p.playEntry("p1", row)
	select {
	case <-h.held:
	case <-time.After(time.Second):
		t.Fatal("the duplicate click did not reach the engine")
	}
	p.playbackErrorFor(h, p.engineGen, taggedPlayError(accepted, "play", "the accepted play failed"))
	close(h.release)
	eventually(t, "the accepted failure", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice != ""
	})
}

// Eight older failures may arrive while one latest click waits to bind. The
// latest accepted command's failure must still be reported.
func TestOnePendingClickKeepsItsFailureAfterOlderErrors(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"

	var older []uint64
	for i := 0; i < 8; i++ {
		older = append(older, asyncPlay(t, p, entry{
			title: "Old",
			uri:   "spotify:album:old" + strconv.Itoa(i),
		}))
	}

	h := &heldCoalescingEngine{
		fakeEngine: &fakeEngine{ready: true},
		held:       make(chan struct{}),
		release:    make(chan struct{}),
	}
	p.engine, p.player = h, h
	row := entry{title: "Album A", uri: "spotify:album:a"}
	accepted := asyncPlay(t, p, row)
	p.playEntry("p1", row)
	select {
	case <-h.held:
	case <-time.After(time.Second):
		t.Fatal("the duplicate click did not reach the engine")
	}
	for _, req := range older {
		p.playbackErrorFor(h, p.engineGen, taggedPlayError(req, "play", "an older accepted play failed"))
	}
	p.playbackErrorFor(h, p.engineGen, taggedPlayError(accepted, "play", "the accepted play failed"))
	close(h.release)
	eventually(t, "the accepted failure", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice != ""
	})
}

// An older click finishing must not clear the pending click that replaced it.
// This is A -> B -> A: the older B completion is stale, while the latest A
// still keeps the accepted A failure for its binding.
func TestAStaleFinishDoesNotClearANewerPendingClick(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"

	aURI := "spotify:album:a"
	p.playEntry("p1", entry{title: "Album A", uri: aURI})
	eventually(t, "the first A to finish", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	p.mu.Lock()
	accepted := p.playReq
	p.mu.Unlock()

	bURI := "spotify:album:b"
	bReq, bSeq, _, cancelB, ok := p.beginPlay("p1", entry{title: "Album B", uri: bURI}, bURI, "")
	if !ok {
		t.Fatal("the B click was rejected")
	}
	p.playbackErrorFor(e, p.engineGen, taggedPlayError(accepted, "play", "the accepted A failed"))
	cancelB()

	aReq, _, _, cancelA, ok := p.beginPlay("p1", entry{title: "Album A", uri: aURI}, aURI, "")
	if !ok {
		t.Fatal("the second A click was rejected")
	}
	defer cancelA()
	p.playbackErrorFor(e, p.engineGen, taggedPlayError(accepted, "play", "the accepted A failed"))

	p.mu.Lock()
	p.finishPlay(bReq, bURI, bSeq)(s, nil)
	p.mu.Unlock()
	p.coalescePlay(aReq, aURI, strconv.FormatUint(accepted, 10))

	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" {
		t.Fatal("the stale B finish cleared the latest A failure")
	}
}

// A deferred failure carries its source. If that source is retired before
// the duplicate binds, the failure must not be replayed onto the replacement
// player, its notice or its mode.
func TestADeferredRetiredSourceErrorIsNotReplayed(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &heldCoalescingEngine{
		fakeEngine: &fakeEngine{ready: true},
		held:       make(chan struct{}),
		release:    make(chan struct{}),
	}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	row := asyncRows[0]
	src := &source{}

	accepted := asyncPlay(t, p, row)
	p.playEntry("p1", row)
	select {
	case <-e.held:
	case <-time.After(time.Second):
		t.Fatal("the duplicate click did not reach the engine")
	}
	p.playbackErrorFor(e, p.engineGen, &sourceError{
		source: src,
		err:    taggedPlayError(accepted, "play", "the retired engine failed"),
	})
	src.retire()
	p.playbackFor(e, p.engineGen, transport.Snapshot{
		State:  transport.StatePlaying,
		Track:  "New inner track",
		URI:    "spotify:track:new",
		ReadAt: time.Now(),
	})

	close(e.release)
	eventually(t, "the duplicate click to bind", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "New inner track" {
		t.Fatalf("track = %q, want the replacement", s.snap.Track)
	}
	if s.notice != "" || s.playback != modePlaying {
		t.Fatalf("a retired source changed the replacement: notice=%q mode=%v", s.notice, s.playback)
	}
}

// A source that is retired before its callback reaches the plugin must not
// set a notice, revert a guess or change the playback mode. A live source's
// current failure still reports.
func TestARetiredSourceErrorCannotChangeTheReplacement(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	e := &fakeEngine{ready: true}
	p.engine, p.player = e, e
	s.snap.URI = "spotify:track:old"
	req := asyncPlay(t, p, asyncRows[0])
	src := &source{}
	retired := &sourceError{source: src, err: taggedPlayError(req, "play", "the old engine failed")}
	src.retire()

	p.playbackErrorFor(e, p.engineGen, retired)
	p.mu.Lock()
	if s.notice != "" || s.guess == nil || s.playback != modePlaying || p.playReq != req {
		p.mu.Unlock()
		t.Fatalf("a retired source changed the pane: notice=%q guess=%+v mode=%v req=%d",
			s.notice, s.guess, s.playback, p.playReq)
	}
	p.mu.Unlock()

	live := &sourceError{source: &source{}, err: taggedPlayError(req, "play", "the current engine failed")}
	p.playbackErrorFor(e, p.engineGen, live)
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice == "" || s.guess != nil {
		t.Fatalf("a live current failure did not report: notice=%q guess=%+v", s.notice, s.guess)
	}
}
