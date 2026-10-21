package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSource is a Source that the test fires by hand, so that a test
// never waits for a real notification.
type fakeSource struct {
	mu      sync.Mutex
	handler func()
	started bool
	closed  bool
	running bool
	err     error
}

// Start records the handler the monitor gave it.
func (s *fakeSource) Start(handler func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.handler = handler
	s.started = true
	s.running = true
	return nil
}

// Close records that the monitor closed it.
func (s *fakeSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.running = false
	return nil
}

// isRunning says whether the source started and did not close since.
func (s *fakeSource) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// fire calls the handler as a notification would.
func (s *fakeSource) fire() {
	s.mu.Lock()
	handler := s.handler
	s.mu.Unlock()
	if handler != nil {
		handler()
	}
}

// isClosed says whether Close ran.
func (s *fakeSource) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// waitFor waits for one value, and fails the test rather than hang if
// the code under test never sends one.
func waitFor[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		var zero T
		t.Fatal("timed out waiting for the monitor")
		return zero
	}
}

// manualTicks replaces a monitor's timer with one the test fires, and
// reports each interval the monitor waits for.
func manualTicks(monitor *Monitor) (intervals chan time.Duration, ticks chan time.Time) {
	intervals = make(chan time.Duration, 64)
	ticks = make(chan time.Time)
	monitor.after = func(interval time.Duration) <-chan time.Time {
		intervals <- interval
		return ticks
	}
	return intervals, ticks
}

func TestMonitorSlowsPollWhileListening(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	source := &fakeSource{}
	monitor := NewMonitor(New(runner), source)
	monitor.Poll = time.Second
	monitor.Safety = 42 * time.Second

	emits := make(chan Snapshot, 8)
	monitor.Emit = func(snapshot Snapshot) { emits <- snapshot }
	intervals, _ := manualTicks(monitor)

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()

	// A source that started is not yet a source that delivers: until a
	// notification arrives, the monitor keeps the fast poll.
	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Poll {
		t.Fatalf("interval before any notification = %v, want %v", got, monitor.Poll)
	}

	// A notification reads at once, and the poll slows.
	source.fire()
	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Safety {
		t.Fatalf("interval after a notification = %v, want %v", got, monitor.Safety)
	}
	if got := len(runner.calls()); got != 2 {
		t.Fatalf("reads = %d, want 2 (first read and the notification)", got)
	}
}

func TestMonitorFallsBackWhenNotificationsStop(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	source := &fakeSource{}
	monitor := NewMonitor(New(runner), source)
	monitor.Poll = time.Second
	monitor.Safety = 42 * time.Second

	emits := make(chan Snapshot, 64)
	monitor.Emit = func(snapshot Snapshot) { emits <- snapshot }
	intervals, ticks := manualTicks(monitor)

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()
	waitFor(t, emits)
	waitFor(t, intervals)

	source.fire()
	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Safety {
		t.Fatalf("interval after a notification = %v, want %v", got, monitor.Safety)
	}

	// Safety intervals pass with no notification. After quietPeriods of
	// them the monitor stops trusting the source and polls fast again.
	for period := 1; period <= quietPeriods; period++ {
		ticks <- time.Time{}
		waitFor(t, emits)
		want := monitor.Safety
		if period == quietPeriods {
			want = monitor.Poll
		}
		if got := waitFor(t, intervals); got != want {
			t.Fatalf("interval after %d quiet periods = %v, want %v", period, got, want)
		}
	}

	// The next notification slows the poll again.
	source.fire()
	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Safety {
		t.Fatalf("interval after a late notification = %v, want %v", got, monitor.Safety)
	}
}

func TestMonitorRereadsAfterCommandDropsARead(t *testing.T) {
	commandStarted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runner := &fakeRunner{run: func(_ context.Context, script string) (string, error) {
		if script != readScript {
			once.Do(func() { close(commandStarted) })
			<-release
			return "", nil
		}
		return playingOutput(), nil
	}}
	player := New(runner)
	source := &fakeSource{}
	monitor := NewMonitor(player, source)
	emits := make(chan Snapshot, 8)
	monitor.Emit = func(snapshot Snapshot) { emits <- snapshot }
	monitor.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()
	waitFor(t, emits)

	commandDone := make(chan error, 1)
	go func() { commandDone <- player.PlayPause(context.Background()) }()
	<-commandStarted

	// Spotify says it changed while the command holds the transport, so
	// the read the notification asks for is dropped.
	source.fire()
	deadline := time.Now().Add(5 * time.Second)
	for !droppedRead(player) {
		if time.Now().After(deadline) {
			t.Fatal("the notification's read was never tried")
		}
		time.Sleep(time.Millisecond)
	}
	if len(emits) != 0 {
		t.Fatal("a dropped read emitted a snapshot")
	}

	// When the command ends, the monitor reads the change it missed.
	close(release)
	if err := <-commandDone; err != nil {
		t.Fatalf("command: %v", err)
	}
	waitFor(t, emits)
	if got := len(runner.calls()); got != 3 {
		t.Fatalf("scripts = %d, want 3 (read, command, read again)", got)
	}
}

// droppedRead says whether a read found the transport busy since its
// slot was last released.
func droppedRead(player *Transport) bool {
	player.mu.Lock()
	defer player.mu.Unlock()
	return player.missed
}

func TestMonitorSendsTheLastSnapshotToANewPane(t *testing.T) {
	var mu sync.Mutex
	reads := 0
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		mu.Lock()
		reads++
		n := reads
		mu.Unlock()
		track := "first"
		if n > 1 {
			track = "second"
		}
		return readOutput("playing", track, "a", "b", "c", "1000", "0", "50", "false", "false"), nil
	}}
	monitor := NewMonitor(New(runner), nil)
	emits := make(chan Snapshot, 8)
	monitor.Emit = func(snapshot Snapshot) { emits <- snapshot }
	monitor.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()
	if got := waitFor(t, emits); got.Track != "first" {
		t.Fatalf("first snapshot = %q, want first", got.Track)
	}

	// A second pane gets the last snapshot at once, then a fresh read,
	// rather than waiting for the next interval.
	monitor.Visible("pane-2", true)
	if got := waitFor(t, emits); got.Track != "first" {
		t.Fatalf("snapshot sent to the new pane = %q, want the last one", got.Track)
	}
	if got := waitFor(t, emits); got.Track != "second" {
		t.Fatalf("snapshot after the new pane = %q, want a fresh read", got.Track)
	}

	// A pane that is already visible asks for nothing.
	monitor.Visible("pane-2", true)
	if got := len(runner.calls()); got != 2 {
		t.Fatalf("reads = %d, want 2", got)
	}
}

func TestMonitorBacksOffWhileSpotifyIsNotInstalled(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return "", &CommandError{
			Stderr: "execution error: Can’t get application \"Spotify\". (-1728)",
			Err:    errors.New("exit status 1"),
		}
	}}
	monitor := NewMonitor(New(runner), nil)
	monitor.Poll = time.Second
	errs := make(chan error, 8)
	monitor.OnError = func(err error) { errs <- err }
	intervals, _ := manualTicks(monitor)

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()

	if err := waitFor(t, errs); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("OnError = %v, want ErrNotInstalled", err)
	}
	if got := waitFor(t, intervals); got != notInstalledInterval {
		t.Fatalf("interval without Spotify = %v, want %v", got, notInstalledInterval)
	}
}

func TestMonitorStartAndCloseDoNotRace(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	source := &fakeSource{}
	monitor := NewMonitor(New(runner), source)
	monitor.Emit = func(Snapshot) {}
	monitor.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	monitor.Visible("pane-1", true)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = monitor.Start(context.Background())
		}()
		go func() {
			defer wg.Done()
			_ = monitor.Close()
		}()
	}
	wg.Wait()

	if err := monitor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if source.isRunning() {
		t.Fatal("a Start that raced a Close left the source listening")
	}
}

func TestMonitorPollsWhenSourceFails(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	source := &fakeSource{err: ErrUnsupported}
	monitor := NewMonitor(New(runner), source)
	monitor.Poll = 3 * time.Second
	monitor.Safety = 40 * time.Second

	emits := make(chan Snapshot, 8)
	monitor.Emit = func(snapshot Snapshot) { emits <- snapshot }
	intervals := make(chan time.Duration, 8)
	never := make(chan time.Time)
	monitor.after = func(interval time.Duration) <-chan time.Time {
		intervals <- interval
		return never
	}

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Start error = %v, want ErrUnsupported", err)
	}
	defer monitor.Close()

	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Poll {
		t.Fatalf("interval without a listener = %v, want %v", got, monitor.Poll)
	}
}

func TestMonitorReadsOnlyWhileVisible(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	monitor := NewMonitor(New(runner), nil)
	emits := make(chan Snapshot, 8)
	monitor.Emit = func(snapshot Snapshot) { emits <- snapshot }
	idle := make(chan struct{}, 4)
	monitor.idle = func() { idle <- struct{}{} }
	monitor.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }

	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()

	// Nobody shows the player, so the monitor waits without reading.
	waitFor(t, idle)
	if got := len(runner.calls()); got != 0 {
		t.Fatalf("reads while hidden = %d, want 0", got)
	}

	// The first visible pane reads at once.
	monitor.Visible("pane-1", true)
	waitFor(t, emits)

	// Hiding it stops the reads again.
	monitor.Visible("pane-1", false)
	waitFor(t, idle)
	if got := len(runner.calls()); got != 1 {
		t.Fatalf("reads after hiding = %d, want 1", got)
	}
}

func TestMonitorCloseClosesItsSource(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	source := &fakeSource{}
	monitor := NewMonitor(New(runner), source)
	monitor.Emit = func(Snapshot) {}
	monitor.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }

	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	monitor.Visible("pane-1", true)

	if err := monitor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !source.isClosed() {
		t.Error("Close did not close the source")
	}
	// Close is safe to call twice.
	if err := monitor.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestMonitorReportsAutomationRefusal(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return "", &CommandError{Stderr: "Not authorized to send Apple events. (-1743)", Err: errors.New("exit status 1")}
	}}
	monitor := NewMonitor(New(runner), nil)
	monitor.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	errs := make(chan error, 8)
	monitor.OnError = func(err error) { errs <- err }

	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()
	monitor.Visible("pane-1", true)

	if err := waitFor(t, errs); !errors.Is(err, ErrAutomation) {
		t.Fatalf("OnError = %v, want ErrAutomation", err)
	}
}
