package transport

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PollInterval is how often a Monitor reads while no Source listens.
const PollInterval = time.Second

// SafetyInterval is how often a Monitor reads while a Source listens.
// The listener covers changes; the poll is a safety net for a
// notification that never arrived.
const SafetyInterval = 10 * time.Second

// Monitor keeps one snapshot of the player current for the panes that
// show it. It reads only while at least one pane is visible, and reads
// at once when a pane becomes visible or Source says Spotify changed.
type Monitor struct {
	// Player is the transport the monitor reads with. It is required.
	Player *Transport

	// Source tells the monitor that Spotify's playback state changed.
	// It may be nil, and then the monitor only polls.
	Source Source

	// Poll is the interval between reads while Source is not running.
	// Zero means PollInterval.
	Poll time.Duration

	// Safety is the interval between reads while Source is running.
	// Zero means SafetyInterval.
	Safety time.Duration

	// Emit receives each snapshot, on the monitor's own goroutine, one
	// snapshot at a time. It must return quickly.
	Emit func(Snapshot)

	// OnError receives each read error, on the monitor's own goroutine.
	// It must return quickly.
	OnError func(error)

	mu       sync.Mutex
	visible  map[string]bool
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	signal   chan struct{}
	done     chan struct{}
	sourceOn bool

	// after returns a channel that fires once after the given time.
	// Tests replace it to see the intervals without waiting for them.
	after func(time.Duration) <-chan time.Time
	// idle is told when the monitor has no visible pane and is about to
	// wait. Tests use it to see that a hidden monitor does not read.
	idle func()
}

// NewMonitor returns a Monitor that reads with player and listens with
// source. A nil source is allowed.
func NewMonitor(player *Transport, source Source) *Monitor {
	return &Monitor{Player: player, Source: source, visible: map[string]bool{}}
}

// Start starts the monitor. It returns the error that Source.Start
// returns, if any, and still polls, because a listener that failed must
// not stop the player from working. Start does not block.
func (m *Monitor) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.signal = make(chan struct{}, 1)
	m.done = make(chan struct{})
	m.mu.Unlock()

	var sourceErr error
	if m.Source != nil {
		sourceErr = m.Source.Start(m.changed)
		m.mu.Lock()
		m.sourceOn = sourceErr == nil
		m.mu.Unlock()
	}

	go m.loop()
	return sourceErr
}

// Close stops the monitor and its source, and waits for the read in
// flight to finish. Close is safe to call more than once.
func (m *Monitor) Close() error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	cancel := m.cancel
	source := m.Source
	done := m.done
	m.mu.Unlock()

	cancel()
	var sourceErr error
	if source != nil {
		sourceErr = source.Close()
	}
	<-done
	return sourceErr
}

// Visible says whether one pane shows the player. The monitor reads
// while at least one pane is visible, and reads at once when the first
// one becomes visible.
func (m *Monitor) Visible(pane string, visible bool) {
	m.mu.Lock()
	was := len(m.visible) > 0
	if visible {
		m.visible[pane] = true
	} else {
		delete(m.visible, pane)
	}
	is := len(m.visible) > 0
	m.mu.Unlock()

	if was != is {
		m.poke()
	}
}

// Showing says whether the monitor reads for a pane, which is true while
// the pane is visible.
func (m *Monitor) Showing(pane string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.visible[pane]
}

// changed is the handler a Source calls when Spotify's state changes.
// It must not block, because the notification arrives on the main
// thread.
func (m *Monitor) changed() {
	m.poke()
}

// poke wakes the loop without waiting when it already has a wake-up
// pending.
func (m *Monitor) poke() {
	m.mu.Lock()
	signal := m.signal
	m.mu.Unlock()
	if signal == nil {
		return
	}
	select {
	case signal <- struct{}{}:
	default:
	}
}

// loop reads while a pane is visible and waits in between.
func (m *Monitor) loop() {
	defer close(m.done)

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		if !m.anyVisible() {
			if m.idle != nil {
				m.idle()
			}
			select {
			case <-m.ctx.Done():
				return
			case <-m.signal:
			}
			continue
		}

		m.read()
		select {
		case <-m.ctx.Done():
			return
		case <-m.signal:
		case <-m.tick(m.interval()):
		}
	}
}

// read takes one snapshot and passes it on.
func (m *Monitor) read() {
	if m.Player == nil {
		if m.OnError != nil {
			m.OnError(errors.New("transport: no player"))
		}
		return
	}

	snapshot, err := m.Player.Read(m.ctx)
	if err != nil {
		// Close cancels the context under a read in flight, and a command
		// from the pane can hold the transport when a read begins. Neither
		// is a failure; the next read tries again.
		if m.OnError != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrBusy) {
			m.OnError(err)
		}
		return
	}
	if m.Emit != nil {
		m.Emit(snapshot)
	}
}

// interval is the wait between reads: the safety interval while a
// listener runs, and the poll interval otherwise.
func (m *Monitor) interval() time.Duration {
	m.mu.Lock()
	on := m.sourceOn
	m.mu.Unlock()

	if on {
		if m.Safety > 0 {
			return m.Safety
		}
		return SafetyInterval
	}
	if m.Poll > 0 {
		return m.Poll
	}
	return PollInterval
}

// tick waits for the interval. It is a function so that tests can see
// the interval without waiting for it.
func (m *Monitor) tick(interval time.Duration) <-chan time.Time {
	if m.after != nil {
		return m.after(interval)
	}
	return time.After(interval)
}

func (m *Monitor) anyVisible() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.visible) > 0
}
