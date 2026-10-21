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
	return nil
}

// Close records that the monitor closed it.
func (s *fakeSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
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

	intervals := make(chan time.Duration, 8)
	never := make(chan time.Time)
	monitor.after = func(interval time.Duration) <-chan time.Time {
		intervals <- interval
		return never
	}

	monitor.Visible("pane-1", true)
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer monitor.Close()

	// The first read comes at once, then the listener's interval.
	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Safety {
		t.Fatalf("interval while listening = %v, want %v", got, monitor.Safety)
	}

	// A notification reads at once, and the poll stays slow.
	source.fire()
	waitFor(t, emits)
	if got := waitFor(t, intervals); got != monitor.Safety {
		t.Fatalf("interval after a notification = %v, want %v", got, monitor.Safety)
	}
	if got := len(runner.calls()); got != 2 {
		t.Fatalf("reads = %d, want 2 (first read and the notification)", got)
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
