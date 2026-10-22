package engine

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
)

// logBuffer is a log destination that tests read while the engine writes.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog sends the standard log to a buffer for the test.
func captureLog(t *testing.T) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return b
}

func TestAStallLogLineReachesThePluginLog(t *testing.T) {
	out := captureLog(t)
	h := start(t)
	h.post(t, `{"type":"log","message":"stall: woke audio at 0ms (seeked), playing again"}`)
	if got := out.String(); !strings.Contains(got, "stall: woke audio at 0ms (seeked), playing again") {
		t.Fatalf("log = %q, want the stall line", got)
	}
}

// waitFor polls until cond holds, for state that another goroutine changes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestADroppedCommandStreamMakesTheEngineNotReadyUntilItIsBack(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.post(t, `{"type":"ready"}`)
	h.snap(t)

	resp, err := http.Get(h.base + "commands")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the stream to open", func() bool {
		h.e.mu.Lock()
		defer h.e.mu.Unlock()
		return h.e.streams == 1
	})
	resp.Body.Close()
	select {
	case err := <-h.errs:
		if !errors.Is(err, ErrStarting) {
			t.Fatalf("stream drop = %v, want ErrStarting", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the plugin was not told that the stream dropped")
	}
	if err := h.e.PlayPause(ctx); !errors.Is(err, ErrStarting) {
		t.Fatalf("PlayPause after the drop = %v, want ErrStarting", err)
	}
	if _, err := h.e.Read(ctx); !errors.Is(err, ErrStarting) {
		t.Fatalf("Read after the drop = %v, want ErrStarting", err)
	}

	// The stream reconnects and the page, whose player stayed connected,
	// says so.
	cmds := h.commands(t)
	h.post(t, `{"type":"stream-up"}`)
	h.snap(t)
	if err := h.e.PlayPause(ctx); err != nil {
		t.Fatalf("PlayPause after the stream came back = %v", err)
	}
	wantCommand(t, cmds, `{"cmd":"toggle"}`)
}

func TestASecondStreamKeepsTheEngineReadyWhenTheFirstEnds(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	first, err := http.Get(h.base + "commands")
	if err != nil {
		t.Fatal(err)
	}
	h.commands(t)
	waitFor(t, "two streams", func() bool {
		h.e.mu.Lock()
		defer h.e.mu.Unlock()
		return h.e.streams == 2
	})
	first.Body.Close()
	waitFor(t, "one stream", func() bool {
		h.e.mu.Lock()
		defer h.e.mu.Unlock()
		return h.e.streams == 1
	})
	if _, err := h.e.Read(context.Background()); err != nil {
		t.Fatalf("Read = %v, want the engine to stay ready", err)
	}
	select {
	case err := <-h.errs:
		t.Fatalf("unexpected error %v", err)
	default:
	}
}

func TestAPlayThatWaitedTooLongIsDroppedAndOthersStay(t *testing.T) {
	now := time.Unix(1000, 0)
	var mu sync.Mutex
	h := &harness{web: &fakeWeb{}, snaps: make(chan transport.Snapshot, 16), errs: make(chan error, 16)}
	h.e = New(Config{
		Web:   h.web,
		Token: func(context.Context) (string, error) { return "tok-1", nil },
		Emit:  func(s transport.Snapshot) { h.snaps <- s },
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		},
	})
	t.Cleanup(func() { _ = h.e.Close() })
	if err := h.e.Start(); err != nil {
		t.Fatal(err)
	}
	u := h.web.page(t)
	h.base = u.Scheme + "://" + u.Host + u.Path
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	ctx := context.Background()

	// Both wait with no stream open, then the clock moves on.
	if err := h.e.Play(ctx, "spotify:track:old"); err != nil {
		t.Fatal(err)
	}
	if err := h.e.Next(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	now = now.Add(staleAfter + time.Second)
	mu.Unlock()
	if err := h.e.Play(ctx, "spotify:track:new"); err != nil {
		t.Fatal(err)
	}

	cmds := h.commands(t)
	wantCommand(t, cmds, `{"cmd":"next"}`)
	wantCommand(t, cmds, `{"cmd":"play","uri":"spotify:track:new"}`)
	select {
	case got := <-cmds:
		t.Fatalf("extra command %s, want the stale play dropped", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPageErrorsAreTypedAndClassified(t *testing.T) {
	h := start(t)
	for _, tc := range []struct {
		body            string
		premium, signIn bool
	}{
		{`{"type":"error","kind":"account_error","message":"Premium required"}`, true, false},
		{`{"type":"error","kind":"command","message":"Spotify answered 403 {\"error\":{\"reason\":\"PREMIUM_REQUIRED\"}}"}`, true, false},
		{`{"type":"error","kind":"authentication_error","message":"Invalid token"}`, false, true},
		{`{"type":"error","kind":"command","message":"Spotify answered 401 expired"}`, false, true},
		{`{"type":"error","kind":"command","message":"Spotify answered 403 Restriction violated"}`, false, false},
	} {
		h.post(t, tc.body)
		var pe *PlaybackError
		if err := <-h.errs; !errors.As(err, &pe) {
			t.Fatalf("%s gave %v, want a PlaybackError", tc.body, err)
		}
		if pe.PremiumRequired() != tc.premium || pe.SignInRequired() != tc.signIn {
			t.Errorf("%s: premium=%t signIn=%t, want %t %t", tc.body, pe.PremiumRequired(), pe.SignInRequired(), tc.premium, tc.signIn)
		}
	}
}

func TestEventsOlderThanTheLastAreIgnoredAndANewPageStartsAgain(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.post(t, `{"type":"ready","page":"a","seq":1}`)
	h.snap(t)
	h.post(t, `{"type":"not-ready","page":"a","seq":3}`)
	if err := <-h.errs; !errors.Is(err, ErrStarting) {
		t.Fatalf("not-ready = %v", err)
	}
	// The ready of seq 2 arrives late: the player is not ready since.
	h.post(t, `{"type":"ready","page":"a","seq":2}`)
	if _, err := h.e.Read(ctx); !errors.Is(err, ErrStarting) {
		t.Fatalf("Read after a late ready = %v, want ErrStarting", err)
	}
	h.post(t, `{"type":"ready","page":"a","seq":4}`)
	h.snap(t)
	if _, err := h.e.Read(ctx); err != nil {
		t.Fatalf("Read after the newer ready = %v", err)
	}
	// A late stream-up must not undo a newer not-ready either.
	h.post(t, `{"type":"not-ready","page":"a","seq":6}`)
	<-h.errs
	h.post(t, `{"type":"stream-up","page":"a","seq":5}`)
	if _, err := h.e.Read(ctx); !errors.Is(err, ErrStarting) {
		t.Fatalf("Read after a late stream-up = %v, want ErrStarting", err)
	}
	// A page that loads again numbers from 1.
	h.post(t, `{"type":"ready","page":"b","seq":1}`)
	h.snap(t)
	if _, err := h.e.Read(ctx); err != nil {
		t.Fatalf("Read after a new page's ready = %v", err)
	}
}

func TestAStreamUpRacingAStreamEndKeepsTheEngineReady(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready","page":"a","seq":1}`)
	h.snap(t)
	n := 1
	old, err := http.Get(h.base + "commands")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		// The page's stream is replaced: the new one opens, the old ends,
		// and the page posts its stream-up, in any order.
		next, err := http.Get(h.base + "commands")
		if err != nil {
			t.Fatal(err)
		}
		n++
		// Half the rounds post the stream-up before the old stream ends, and
		// half after, as the two can come in either order.
		up := `{"type":"stream-up","page":"a","seq":` + strconv.Itoa(n) + `}`
		if i%2 == 0 {
			h.post(t, up)
			old.Body.Close()
		} else {
			old.Body.Close()
			h.post(t, up)
		}
		h.snap(t)
		old = next
	}
	defer old.Body.Close()
	waitFor(t, "one stream", func() bool {
		h.e.mu.Lock()
		defer h.e.mu.Unlock()
		return h.e.streams == 1
	})
	if _, err := h.e.Read(context.Background()); err != nil {
		t.Fatalf("Read = %v, want the engine ready", err)
	}
}
