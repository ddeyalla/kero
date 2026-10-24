package engine

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
)

// fakeWeb records the page the engine asked the host to run.
type fakeWeb struct {
	mu     sync.Mutex
	opened []string
	closed []string
	err    error
}

func (f *fakeWeb) WebOpen(id, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, id+" "+url)
	return f.err
}

func (f *fakeWeb) WebClose(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
	return nil
}

func (f *fakeWeb) page(t *testing.T) *url.URL {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opened) != 1 {
		t.Fatalf("the host ran %v, want one page", f.opened)
	}
	u, err := url.Parse(strings.Fields(f.opened[0])[1])
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type harness struct {
	e     *Engine
	web   *fakeWeb
	base  string
	snaps chan transport.Snapshot
	errs  chan error
}

func start(t *testing.T) *harness {
	t.Helper()
	h := &harness{web: &fakeWeb{}, snaps: make(chan transport.Snapshot, 16), errs: make(chan error, 16)}
	h.e = New(Config{
		Web:     h.web,
		Token:   func(context.Context) (string, error) { return "tok-1", nil },
		Emit:    func(s transport.Snapshot) { h.snaps <- s },
		OnError: func(err error) { h.errs <- err },
		Now:     func() time.Time { return time.Unix(1000, 0) },
	})
	t.Cleanup(func() { _ = h.e.Close() })
	if err := h.e.Start(); err != nil {
		t.Fatal(err)
	}
	u := h.web.page(t)
	h.base = u.Scheme + "://" + u.Host + u.Path
	return h
}

func (h *harness) post(t *testing.T, body string) {
	t.Helper()
	resp, err := http.Post(h.base+"events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("post %s = %d", body, resp.StatusCode)
	}
}

func (h *harness) snap(t *testing.T) transport.Snapshot {
	t.Helper()
	select {
	case s := <-h.snaps:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("no snapshot was pushed")
		return transport.Snapshot{}
	}
}

// commands opens the page's command stream and returns the lines of data.
func (h *harness) commands(t *testing.T) <-chan string {
	t.Helper()
	resp, err := http.Get(h.base + "commands")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	out := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				out <- data
			}
		}
	}()
	return out
}

func wantCommand(t *testing.T, cmds <-chan string, want string) {
	t.Helper()
	select {
	case got := <-cmds:
		if got != want {
			t.Fatalf("command = %s, want %s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no command, want %s", want)
	}
}

func TestStartAsksTheHostForALoopbackPageWithAKey(t *testing.T) {
	h := start(t)
	u := h.web.page(t)
	if u.Scheme != "http" || !strings.HasPrefix(u.Host, "127.0.0.1:") {
		t.Fatalf("page = %s, want http on the loopback", u)
	}
	if !strings.HasPrefix(u.Path, "/e/") || len(u.Path) < 30 || u.Query().Get("name") != "Shibu Code" {
		t.Fatalf("page path/query = %s", u)
	}
	resp, err := http.Get(h.base)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("page = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// A second Start does not run another page.
	if err := h.e.Start(); err != nil || len(h.web.opened) != 1 {
		t.Fatalf("second start = %v, pages %v", err, h.web.opened)
	}
}

func TestServerRefusesWhatIsNotTheEnginesPage(t *testing.T) {
	h := start(t)
	u := h.web.page(t)
	other := u.Scheme + "://" + u.Host + "/e/not-the-key/token"
	if resp, err := http.Get(other); err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a wrong key answered %v %v, want 404", resp, err)
	}
	for name, edit := range map[string]func(*http.Request){
		"a rebound host":    func(r *http.Request) { r.Host = "evil.example:" + u.Port() },
		"another origin":    func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"the origin's port": func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:1") },
	} {
		req, _ := http.NewRequest("GET", h.base+"token", nil)
		edit(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s got %d, want 403", name, resp.StatusCode)
		}
	}
	// The page's own origin is fine.
	req, _ := http.NewRequest("GET", h.base+"token", nil)
	req.Header.Set("Origin", "http://"+u.Host)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("the page's own origin got %v %v", resp, err)
	}
	// Only a post reaches the events.
	if resp, err := http.Get(h.base + "events"); err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("a get of the events answered %v %v", resp, err)
	}
}

func TestTokenGoesToThePageAndAFailureIsA503(t *testing.T) {
	h := start(t)
	resp, err := http.Get(h.base + "token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if got := strings.TrimSpace(string(buf[:n])); got != `{"token":"tok-1"}` {
		t.Fatalf("token = %s", got)
	}

	h.e.cfg.Token = func(context.Context) (string, error) { return "", errors.New("signed out") }
	resp2, err := http.Get(h.base + "token")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a failed token answered %d, want 503", resp2.StatusCode)
	}
}

func TestPlayerPushesItsStateAndTakesCommands(t *testing.T) {
	h := start(t)
	ctx := context.Background()

	// Before the player is ready, nothing is read and nothing is sent.
	if _, err := h.e.Read(ctx); !errors.Is(err, ErrStarting) {
		t.Fatalf("Read before ready = %v, want ErrStarting", err)
	}
	if err := h.e.PlayPause(ctx); !errors.Is(err, ErrStarting) {
		t.Fatalf("PlayPause before ready = %v, want ErrStarting", err)
	}

	cmds := h.commands(t)
	h.post(t, `{"type":"ready"}`)
	if s := h.snap(t); s.State != transport.StateStopped {
		t.Fatalf("ready snapshot = %+v", s)
	}
	h.post(t, `{"type":"volume","value":35}`)
	if s := h.snap(t); s.Volume != 35 {
		t.Fatalf("volume snapshot = %+v", s)
	}
	h.post(t, `{"type":"state","active":true,"paused":false,"position":1500,"duration":302760,"track":"Juicy","artist":"The Notorious B.I.G.","album":"Ready to Die","image":"https://i.scdn.co/a","shuffle":true,"repeat":1}`)
	s := h.snap(t)
	if s.State != transport.StatePlaying || s.Track != "Juicy" || s.Artist != "The Notorious B.I.G." || s.Album != "Ready to Die" ||
		s.ArtworkURL != "https://i.scdn.co/a" || s.Position != 1500*time.Millisecond || s.Duration != 302760*time.Millisecond ||
		!s.Shuffle || !s.Repeat || s.Volume != 35 || !s.ReadAt.Equal(time.Unix(1000, 0)) {
		t.Fatalf("state snapshot = %+v", s)
	}
	if got, err := h.e.Read(ctx); err != nil || got != s {
		t.Fatalf("Read = %+v, %v, want the pushed snapshot", got, err)
	}
	h.post(t, `{"type":"state","active":true,"paused":true,"position":9,"duration":10,"track":"T"}`)
	if s := h.snap(t); s.State != transport.StatePaused || s.Shuffle || s.Repeat {
		t.Fatalf("paused snapshot = %+v", s)
	}
	// A player that no longer holds the playback is stopped, and keeps
	// its volume.
	h.post(t, `{"type":"state","active":false}`)
	if s := h.snap(t); s.State != transport.StateStopped || s.Track != "" || s.Volume != 35 {
		t.Fatalf("inactive snapshot = %+v", s)
	}

	for _, c := range []struct {
		do   func() error
		want string
	}{
		{func() error { return h.e.PlayPause(ctx) }, `{"cmd":"toggle"}`},
		{func() error { return h.e.Next(ctx) }, `{"cmd":"next"}`},
		{func() error { return h.e.Previous(ctx) }, `{"cmd":"previous"}`},
		{func() error { return h.e.Seek(ctx, 90*time.Second) }, `{"cmd":"seek","ms":90000}`},
		{func() error { return h.e.Volume(ctx, 60) }, `{"cmd":"volume","value":60}`},
		{func() error { return h.e.Shuffle(ctx, true) }, `{"cmd":"shuffle","on":true}`},
		{func() error { return h.e.Repeat(ctx, false) }, `{"cmd":"repeat","on":false}`},
		{func() error { return h.e.Play(ctx, "spotify:album:4LH4d3cOWNNsVw41Gqt2kv") }, `{"cmd":"play","uri":"spotify:album:4LH4d3cOWNNsVw41Gqt2kv"}`},
	} {
		if err := c.do(); err != nil {
			t.Fatalf("%s: %v", c.want, err)
		}
		wantCommand(t, cmds, c.want)
	}
}

func TestCommandsAreCheckedBeforeTheyAreSent(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	ctx := context.Background()
	if err := h.e.Volume(ctx, 101); !errors.Is(err, transport.ErrRange) {
		t.Errorf("Volume(101) = %v, want ErrRange", err)
	}
	if err := h.e.Seek(ctx, -time.Second); !errors.Is(err, transport.ErrRange) {
		t.Errorf("Seek(-1s) = %v, want ErrRange", err)
	}
	for _, uri := range []string{"", "spotify:", "https://open.spotify.com/album/x", "spotify:album:a\"b", "spotify:user:x:playlist:y", "spotify:track:"} {
		if err := h.e.Play(ctx, uri); !errors.Is(err, transport.ErrURI) {
			t.Errorf("Play(%q) = %v, want ErrURI", uri, err)
		}
	}
	if err := h.e.Launch(ctx); !errors.Is(err, transport.ErrNotInstalled) {
		t.Errorf("Launch = %v, want ErrNotInstalled: the engine has no app", err)
	}
}

func TestPlayerErrorsAndLossReachThePlugin(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	h.post(t, `{"type":"error","kind":"account_error","message":"Premium required"}`)
	if err := <-h.errs; !strings.Contains(err.Error(), "account_error") || !strings.Contains(err.Error(), "Premium required") {
		t.Fatalf("error = %v", err)
	}
	h.post(t, `{"type":"not-ready"}`)
	if err := <-h.errs; !errors.Is(err, ErrStarting) {
		t.Fatalf("not-ready = %v, want ErrStarting", err)
	}
	if _, err := h.e.Read(context.Background()); !errors.Is(err, ErrStarting) {
		t.Fatalf("Read after not-ready = %v, want ErrStarting", err)
	}
}

func TestCloseEndsThePageAndTheServer(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	if err := h.e.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatal(err)
	}
	if err := h.e.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	if len(h.web.closed) != 1 || h.web.closed[0] != webID {
		t.Fatalf("the host closed %v, want the engine's web view once", h.web.closed)
	}
	if _, err := http.Get(h.base); err == nil {
		t.Fatal("the server still answers after Close")
	}
	ctx := context.Background()
	if _, err := h.e.Read(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("Read after Close = %v, want ErrClosed", err)
	}
	if err := h.e.Next(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("Next after Close = %v, want ErrClosed", err)
	}
	if err := h.e.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close = %v, want ErrClosed", err)
	}
}

func TestAHostThatRefusesThePageStopsTheEngine(t *testing.T) {
	web := &fakeWeb{err: errors.New("web.open: refused")}
	e := New(Config{Web: web, Token: func(context.Context) (string, error) { return "", nil }})
	if err := e.Start(); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("Start = %v, want the host's refusal", err)
	}
	if _, err := e.Read(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Read = %v, want ErrClosed: a refused engine does not linger", err)
	}
}

func TestABurstOfSeeksAndVolumesCoalescesToTheLatest(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	ctx := context.Background()

	// The page does not read yet, as when it is busy: the burst waits.
	for i := 1; i <= 49; i++ {
		if err := h.e.Seek(ctx, time.Duration(i)*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []int{16, 32, 48} {
		if err := h.e.Volume(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.e.Next(ctx); err != nil {
		t.Fatal(err)
	}

	cmds := h.commands(t)
	wantCommand(t, cmds, `{"cmd":"seek","ms":49000}`)
	wantCommand(t, cmds, `{"cmd":"volume","value":48}`)
	wantCommand(t, cmds, `{"cmd":"next"}`)
	select {
	case got := <-cmds:
		t.Fatalf("extra command %s after the burst", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestACommandBetweenTwoSeeksKeepsTheOrder(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	ctx := context.Background()

	if err := h.e.Seek(ctx, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := h.e.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.e.Seek(ctx, 20*time.Second); err != nil {
		t.Fatal(err)
	}

	cmds := h.commands(t)
	wantCommand(t, cmds, `{"cmd":"seek","ms":10000}`)
	wantCommand(t, cmds, `{"cmd":"next"}`)
	wantCommand(t, cmds, `{"cmd":"seek","ms":20000}`)
	select {
	case got := <-cmds:
		t.Fatalf("extra command %s", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTheStateCarriesTheTrackURI(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	h.post(t, `{"type":"state","active":true,"paused":false,"track":"Juicy","uri":"spotify:track:abc"}`)
	if got := h.snap(t); got.URI != "spotify:track:abc" {
		t.Fatalf("URI = %q, want the track's", got.URI)
	}
}

func TestADoublePlayWithinTheWindowIsDropped(t *testing.T) {
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
	cmds := h.commands(t)
	ctx := context.Background()
	a, b := "spotify:track:aaa", "spotify:track:bbb"

	for _, uri := range []string{a, a, b} {
		if err := h.e.Play(ctx, uri); err != nil {
			t.Fatal(err)
		}
	}
	wantCommand(t, cmds, `{"cmd":"play","uri":"`+a+`"}`)
	wantCommand(t, cmds, `{"cmd":"play","uri":"`+b+`"}`)

	// Past the window the same URI is a new press.
	mu.Lock()
	now = now.Add(2 * time.Second)
	mu.Unlock()
	if err := h.e.Play(ctx, b); err != nil {
		t.Fatal(err)
	}
	wantCommand(t, cmds, `{"cmd":"play","uri":"`+b+`"}`)
	select {
	case got := <-cmds:
		t.Fatalf("extra command %s", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPlayFromSendsTheOffsetAndItsDoubleClickIsDropped(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	cmds := h.commands(t)
	album := "spotify:album:abc"

	for _, track := range []string{"spotify:track:one", "spotify:track:one", "spotify:track:two"} {
		if err := h.e.PlayFrom(ctx, album, track); err != nil {
			t.Fatal(err)
		}
	}
	// Two tracks of one album are two presses; the same one twice is one.
	wantCommand(t, cmds, `{"cmd":"play","offset":"spotify:track:one","uri":"`+album+`"}`)
	wantCommand(t, cmds, `{"cmd":"play","offset":"spotify:track:two","uri":"`+album+`"}`)
	select {
	case got := <-cmds:
		t.Fatalf("extra command %s", got)
	case <-time.After(100 * time.Millisecond):
	}

	for _, bad := range [][2]string{{"nonsense", "spotify:track:one"}, {album, "spotify:album:abc"}, {album, ""}} {
		if err := h.e.PlayFrom(ctx, bad[0], bad[1]); !errors.Is(err, transport.ErrURI) {
			t.Errorf("PlayFrom(%q, %q) = %v, want ErrURI", bad[0], bad[1], err)
		}
	}
}

// A command whose context is already over is not admitted: the page never
// sees it.
func TestCancelledCommandsAreNotAdmitted(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	cmds := h.commands(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, fn := range map[string]func() error{
		"play":      func() error { return h.e.Play(ctx, "spotify:track:t1") },
		"play-from": func() error { return h.e.PlayFrom(ctx, "spotify:album:a1", "spotify:track:t1") },
		"toggle":    func() error { return h.e.PlayPause(ctx) },
	} {
		if err := fn(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s = %v, want context.Canceled", name, err)
		}
	}
	select {
	case got := <-cmds:
		t.Fatalf("the page received %q from cancelled commands", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// admissionProbe tells when its context was first consulted, so a test can
// cancel while a command waits for the engine's state lock.
type admissionProbe struct {
	context.Context
	once    sync.Once
	checked chan struct{}
}

func (c *admissionProbe) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

// A command cancelled while it waits for the engine's state lock must not
// be queued when the lock is released.
func TestCancelledWhileWaitingForAdmissionQueuesNothing(t *testing.T) {
	inner, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &admissionProbe{Context: inner, checked: make(chan struct{})}
	e := New(Config{Now: time.Now})
	e.mu.Lock()
	e.ready = true
	done := make(chan error, 1)
	go func() { done <- e.Play(ctx, "spotify:track:review") }()
	<-ctx.checked
	cancel()
	e.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("play admitted after cancellation while waiting for the lock: %v", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.queue) != 0 {
		t.Errorf("cancelled call queued %d commands", len(e.queue))
	}
}

// A command admitted before the caller cancels still runs: the plugin's
// action defers cancel as soon as Play returns.
func TestAcceptedCommandSurvivesCallerCancellation(t *testing.T) {
	h := start(t)
	h.post(t, `{"type":"ready"}`)
	h.snap(t)
	ctx, cancel := context.WithCancel(context.Background())
	if err := h.e.Play(ctx, "spotify:track:kept"); err != nil {
		t.Fatalf("play: %v", err)
	}
	cancel()
	h.e.mu.Lock()
	defer h.e.mu.Unlock()
	if len(h.e.queue) != 1 {
		t.Fatalf("queue = %d, want the accepted play kept", len(h.e.queue))
	}
}
