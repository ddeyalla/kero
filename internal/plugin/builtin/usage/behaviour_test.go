package usage

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/internal/usage/cost"
)

// savedStates records what a pane saved.
type savedStates struct {
	mu    sync.Mutex
	saved []paneState
}

func (s *savedStates) save(_ string, state any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, state.(paneState))
	return nil
}

func (s *savedStates) last() (paneState, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.saved) == 0 {
		return paneState{}, 0
	}
	return s.saved[len(s.saved)-1], len(s.saved)
}

func TestUsageEventsSaveAndRestore(t *testing.T) {
	src := newFake(time.Now())
	p := newPlugin(src)
	rec := &savedStates{}
	p.save = rec.save
	addPane(p, "p1", protocol.Size{Width: 640, Height: 480})

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "usage-range", Kind: protocol.KindSelect, Value: "30d"})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "toggle-codex", Kind: protocol.KindPress})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "toggle-claude", Kind: protocol.KindPress})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "toggle-claude", Kind: protocol.KindPress})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "usage-range", Kind: protocol.KindSelect, Value: "bogus"})
	got, n := rec.last()
	if n != 4 || got.Range != "30d" || len(got.Open) != 1 || got.Open[0] != "codex" {
		t.Fatalf("saved %d times, last %+v", n, got)
	}
	v := p.snapshot("p1")
	if v.rng != range30Days || !v.open["codex"] || v.open["claude"] {
		t.Fatalf("the view is %q %v", v.rng, v.open)
	}

	raw, _ := json.Marshal(got)
	q := newPlugin(newFake(time.Now()))
	q.open(nil, sdk.OpenEvent{Pane: "p2", State: raw, Size: protocol.Size{Width: 640, Height: 480}})
	v = q.snapshot("p2")
	if v.rng != range30Days || !v.open["codex"] {
		t.Fatalf("the restored view is %q %v", v.rng, v.open)
	}
	q.open(nil, sdk.OpenEvent{Pane: "p3", State: json.RawMessage(`{"range":"nope"}`)})
	if v = q.snapshot("p3"); v.rng != rangeToday || len(v.open) != 0 {
		t.Fatalf("a bad state gave %q %v", v.rng, v.open)
	}
}

func TestUsageRangeSwitchDoesNotScan(t *testing.T) {
	src := newFake(time.Now())
	p := newPlugin(src)
	p.save = (&savedStates{}).save
	addPane(p, "p1", protocol.Size{Width: 640, Height: 480})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "usage-range", Kind: protocol.KindSelect, Value: "yesterday"})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "toggle-claude", Kind: protocol.KindPress})
	if src.spendCount() != 0 {
		t.Fatalf("a choice read the cost %d times", src.spendCount())
	}
	if _, c := src.counts(); c != 0 {
		t.Fatalf("a choice scanned the logs %d times", c)
	}
}

func TestUsageDoesNotReadWhileHidden(t *testing.T) {
	src := newFake(time.Now())
	p := newPlugin(src)
	p.usageEvery, p.costWait = 5*time.Millisecond, 5*time.Millisecond
	addPane(p, "p1", protocol.Size{Width: 300, Height: 400})
	p.setVisible(nil, "p1", false)
	time.Sleep(60 * time.Millisecond)
	r, c := src.counts()
	if r != 0 || c != 0 || src.spendCount() != 0 {
		t.Fatalf("a hidden pane read: %d accounts, %d logs, %d spends", r, c, src.spendCount())
	}
}

func TestUsageReadsOnlyWhileVisible(t *testing.T) {
	src := newFake(time.Now())
	p := newPlugin(src)
	p.usageEvery, p.costWait = 10*time.Millisecond, 10*time.Millisecond
	addPane(p, "p1", protocol.Size{Width: 300, Height: 400})
	p.setVisible(nil, "p1", true)
	waitFor(t, "reads", func() bool { r, c := src.counts(); return r >= 2 && c >= 2 })
	p.mu.Lock()
	loaded := p.costLoaded
	p.mu.Unlock()
	if !loaded {
		waitFor(t, "the cost", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.costLoaded })
	}
	p.setVisible(nil, "p1", false)
	p.work.Wait()
	r, c := src.counts()
	time.Sleep(80 * time.Millisecond)
	if r2, c2 := src.counts(); r2 != r || c2 != c {
		t.Fatalf("a hidden pane kept reading: %d/%d accounts, %d/%d logs", r, r2, c, c2)
	}
}

func TestUsageCostKeepsItsInterval(t *testing.T) {
	src := newFake(time.Now())
	clock := &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)}
	p := newPlugin(src)
	p.clock = clock.Now
	addPane(p, "p1", protocol.Size{Width: 300, Height: 400})

	p.setVisible(nil, "p1", true)
	waitFor(t, "the first scan", func() bool { _, c := src.counts(); return c == 1 })
	waitFor(t, "it to land", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return !p.costRefreshing })
	p.setVisible(nil, "p1", false)
	p.work.Wait()

	clock.Add(2 * time.Minute)
	showAndHide(p, "p1")
	if _, c := src.counts(); c != 1 {
		t.Fatalf("a pane shown within the interval scanned: %d", c)
	}
	clock.Add(4 * time.Minute)
	p.setVisible(nil, "p1", true)
	waitFor(t, "the scan after the interval", func() bool { _, c := src.counts(); return c == 2 })
	waitFor(t, "it to land", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return !p.costRefreshing })

	p.event(nil, protocol.EventParams{Kind: protocol.KindPress, Node: "usage-refresh"})
	waitFor(t, "the scan the button asks for", func() bool { _, c := src.counts(); return c == 3 })
	p.setVisible(nil, "p1", false)
	p.work.Wait()
}

func TestUsageCostDueAtMidnight(t *testing.T) {
	p := newPlugin(newFake(time.Now()))
	clock := &fakeClock{now: time.Date(2026, 10, 21, 23, 58, 0, 0, time.Local)}
	p.clock = clock.Now
	if got := p.costDue(); got != 0 {
		t.Fatalf("never read is due in %v", got)
	}
	p.lastCost = clock.Now()
	if got := p.costDue(); got != 2*time.Minute {
		t.Fatalf("a read at 23:58 is due in %v, want 2m", got)
	}
	clock.now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)
	p.lastCost = clock.now
	if got := p.costDue(); got != costEvery {
		t.Fatalf("a read at noon is due in %v, want %v", got, costEvery)
	}
}

func TestUsageCloseAllCancelsTheScan(t *testing.T) {
	src := newFake(time.Now())
	src.costDelay = time.Minute
	p := newPlugin(src)
	addPane(p, "p1", protocol.Size{Width: 300, Height: 400})
	p.setVisible(nil, "p1", true)
	waitFor(t, "the scan to start", func() bool { _, c := src.counts(); return c == 1 })

	done := make(chan struct{})
	go func() { p.closeAll(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closeAll waited for the scan")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.costRefreshing || !p.lastCost.IsZero() {
		t.Fatalf("a cancelled scan left refreshing=%v lastCost=%v", p.costRefreshing, p.lastCost)
	}
}

func TestUsageShowsTheCachedCostFirst(t *testing.T) {
	src := newFake(time.Now())
	src.costDelay = time.Minute
	p := newPlugin(src)
	addPane(p, "p1", protocol.Size{Width: 300, Height: 400})
	p.setVisible(nil, "p1", true)
	waitFor(t, "the cache", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.costLoaded && len(p.costs[cost.Today]) == 2
	})
	p.closeAll()
}

func TestUsageCloseStops(t *testing.T) {
	src := newFake(time.Now())
	p := newPlugin(src)
	p.usageEvery, p.costWait = 10*time.Millisecond, 10*time.Millisecond
	addPane(p, "p1", protocol.Size{Width: 300, Height: 400})
	p.setVisible(nil, "p1", true)
	waitFor(t, "a read", func() bool { r, _ := src.counts(); return r >= 1 })
	p.close(nil, "p1")
	p.work.Wait()
	r, c := src.counts()
	time.Sleep(80 * time.Millisecond)
	if r2, c2 := src.counts(); r2 != r || c2 != c {
		t.Fatalf("a closed pane kept reading")
	}
}

// A pane hidden and shown again while the reads are out must not wait out
// the intervals: the reads the first loop cancelled start again at once.
func TestUsageShowAgainDuringReads(t *testing.T) {
	src := newFake(time.Now())
	src.delay, src.costDelay = 400*time.Millisecond, 400*time.Millisecond
	p := newPlugin(src)
	addPane(p, "p1", protocol.Size{Width: 640, Height: 480})
	p.setVisible(nil, "p1", true)
	waitFor(t, "the first reads", func() bool {
		r, c := src.counts()
		return r == 1 && c == 1
	})
	p.setVisible(nil, "p1", false)
	p.setVisible(nil, "p1", true)
	waitFor(t, "the reads again", func() bool {
		r, c := src.counts()
		return r >= 2 && c >= 2
	})
	p.closeAll()
	src.mu.Lock()
	flushes := src.flushes
	src.mu.Unlock()
	if flushes != 1 {
		t.Fatalf("closeAll flushed the cost %d times, want once", flushes)
	}
}
