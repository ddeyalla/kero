package transport

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PollInterval is how often a Monitor reads while no Source is heard.
const PollInterval = time.Second

// SafetyInterval is how often a Monitor reads while its Source delivers
// notifications. The notifications cover changes; the poll is a safety
// net for one that never arrived.
const SafetyInterval = 10 * time.Second

// quietPeriods is how many safety intervals may pass without a
// notification before a Monitor stops trusting its Source and polls at
// PollInterval again, until the next notification arrives. Spotify is
// silent while a long track plays, so the count is generous: a change
// it misses still shows within one safety interval.
const quietPeriods = 6

// notInstalledInterval is how often a Monitor reads while Spotify is not
// installed, so that a missing app does not cost a script and an error
// every second. A pane that becomes visible still reads at once.
const notInstalledInterval = time.Minute

// Monitor keeps one snapshot of the player current for the panes that
// show it. It reads only while at least one pane is visible, and reads
// at once when a pane becomes visible, when Source says Spotify changed,
// and when a call that stopped a read ends.
type Monitor struct {
	// Player is the transport the monitor reads with. It is required.
	Player *Transport

	// Source tells the monitor that Spotify's playback state changed.
	// It may be nil, and then the monitor only polls.
	Source Source

	// Poll is the interval between reads while no notification is
	// heard. Zero means PollInterval.
	Poll time.Duration

	// Safety is the interval between reads while Source delivers
	// notifications. Zero means SafetyInterval.
	Safety time.Duration

	// Emit receives each snapshot, on the monitor's own goroutine, one
	// snapshot at a time. It must return quickly.
	Emit func(Snapshot)

	// OnError receives each read error, on the monitor's own goroutine.
	// It must return quickly.
	OnError func(error)

	// life serializes Start and Close, so that a Close during a Start
	// waits for the Start, and closes the source the Start started.
	life sync.Mutex

	mu      sync.Mutex
	visible map[string]bool
	started bool
	cancel  context.CancelFunc
	signal  chan struct{}
	done    chan struct{}

	// heard says a notification arrived since the Source started, and
	// quiet counts the safety intervals that passed since the last one.
	// The monitor reads at the safety interval only while heard and
	// quiet is below quietPeriods.
	heard bool
	quiet int
	// resend says a pane became visible since the loop last woke. The
	// loop sends it the last snapshot before it reads a new one.
	resend bool

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

	m.life.Lock()
	defer m.life.Unlock()

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	ctx, cancel := context.WithCancel(ctx)
	signal := make(chan struct{}, 1)
	done := make(chan struct{})
	m.cancel = cancel
	m.signal = signal
	m.done = done
	m.heard = false
	m.quiet = 0
	m.mu.Unlock()

	// A read that a command stopped reads again when the command ends.
	if m.Player != nil {
		m.Player.setOnFree(m.poke)
	}

	// The source's handler takes m.mu, so the source starts outside it.
	var sourceErr error
	if m.Source != nil {
		sourceErr = m.Source.Start(m.changed)
	}

	go m.loop(ctx, signal, done)
	return sourceErr
}

// Close stops the monitor and its source, and waits for the read in
// flight to finish. Close is safe to call more than once.
func (m *Monitor) Close() error {
	m.life.Lock()
	defer m.life.Unlock()

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
	if m.Player != nil {
		m.Player.setOnFree(nil)
	}
	var sourceErr error
	if source != nil {
		sourceErr = source.Close()
	}
	<-done
	return sourceErr
}

// Visible says whether one pane shows the player. The monitor reads
// while at least one pane is visible. Each pane that becomes visible
// gets the last snapshot at once, and the monitor reads again.
func (m *Monitor) Visible(pane string, visible bool) {
	m.mu.Lock()
	if m.visible == nil {
		m.visible = map[string]bool{}
	}
	was := len(m.visible) > 0
	added := visible && !m.visible[pane]
	if visible {
		m.visible[pane] = true
	} else {
		delete(m.visible, pane)
	}
	is := len(m.visible) > 0
	if added {
		m.resend = true
	}
	m.mu.Unlock()

	// The last pane to hide wakes the loop too, so that it stops waiting
	// for a read nobody shows.
	if added || was != is {
		m.poke()
	}
}

// changed is the handler a Source calls when Spotify's state changes.
// It must not block, because the notification arrives on the main
// thread. It is the only place that marks a notification heard: the
// other pokes are not news from Spotify.
func (m *Monitor) changed() {
	m.mu.Lock()
	m.heard = true
	m.quiet = 0
	m.mu.Unlock()
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

// loop reads while a pane is visible and waits in between. It takes its
// context and channels from Start, so a later Start never shares them.
func (m *Monitor) loop(ctx context.Context, signal <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	var last Snapshot
	var haveLast bool
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !m.anyVisible() {
			if m.idle != nil {
				m.idle()
			}
			select {
			case <-ctx.Done():
				return
			case <-signal:
			}
			continue
		}

		if m.takeResend() && haveLast && m.Emit != nil {
			m.Emit(last)
		}
		snapshot, err := m.read(ctx)
		if err == nil {
			last, haveLast = snapshot, true
		}

		select {
		case <-ctx.Done():
			return
		case <-signal:
		case <-m.tick(m.interval(err)):
			m.tickPassed()
		}
	}
}

// read takes one snapshot and passes it on. It returns the snapshot, or
// the error that stopped it.
func (m *Monitor) read(ctx context.Context) (Snapshot, error) {
	if m.Player == nil {
		err := errors.New("transport: no player")
		if m.OnError != nil {
			m.OnError(err)
		}
		return Snapshot{}, err
	}

	snapshot, err := m.Player.Read(ctx)
	if err != nil {
		// Close cancels the context under a read in flight, and a command
		// from the pane can hold the transport when a read begins. Neither
		// is a failure: the transport pokes the monitor when the command
		// ends, and the next read tries again.
		if m.OnError != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrBusy) {
			m.OnError(err)
		}
		return Snapshot{}, err
	}
	if m.Emit != nil {
		m.Emit(snapshot)
	}
	return snapshot, nil
}

// takeResend says whether a pane became visible since the last call.
func (m *Monitor) takeResend() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	resend := m.resend
	m.resend = false
	return resend
}

// tickPassed counts an interval that passed without a wake-up. Past
// quietPeriods of them with no notification, interval falls back to the
// poll.
func (m *Monitor) tickPassed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.heard && m.quiet < quietPeriods {
		m.quiet++
	}
}

// interval is the wait after a read that ended with err: a long one
// while Spotify is not installed, the safety interval while
// notifications arrive, and the poll interval otherwise. A Source that
// started but never delivers keeps the poll interval.
func (m *Monitor) interval(err error) time.Duration {
	if errors.Is(err, ErrNotInstalled) {
		return notInstalledInterval
	}

	m.mu.Lock()
	safe := m.heard && m.quiet < quietPeriods
	m.mu.Unlock()

	if safe {
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
