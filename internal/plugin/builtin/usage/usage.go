package usage

// The usage plugin: the account allowances of the coding agents on this Mac,
// and what they cost, as a pane of their own. The plugin has no network code
// of its own; it reads the accounts through internal/usage and the cost from
// the agents' logs through internal/usage/cost, and it refreshes only while
// a pane of it is on screen.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
	accountusage "kero/internal/usage"
	"kero/internal/usage/cost"
)

func init() {
	registry.RegisterRunner("usage", func() error { return serve(newSource()) })
}

const (
	// refreshEvery is how often a visible pane reads the accounts again, the
	// same two minutes the Usage tab keeps.
	refreshEvery = 2 * time.Minute

	// costEvery is how often a visible pane reads the logs again. The read
	// is incremental, but it walks the files, so it is rarer than the
	// accounts'.
	costEvery = 5 * time.Minute

	// compactWidth and compactHeight are where a pane stops being its large
	// self: the same widget draws one row for each account, or the cost card
	// and a section for each account. Below stackedWidth the cost card puts
	// its legend under the donut.
	compactWidth  = 260
	compactHeight = 300
	stackedWidth  = 340
)

// plugin is the usage plugin's process: one pane or a few, and the accounts
// and the cost they share.
type plugin struct {
	src                  Source
	usageEvery, costWait time.Duration
	clock                func() time.Time

	// save keeps a pane's state in its target. It is the connection's
	// SaveState once a pane opens; a test replaces it.
	save func(pane string, state any) error

	mu        sync.Mutex
	conn      *sdk.Conn
	panes     map[string]*pane
	reports   []accountusage.Report
	providers []accountusage.Report // every account the app can read, set up or not
	installed []string
	loading   bool

	// costs is the spend of each range as of the last read of the logs, and
	// costLoaded tells that something was read, so that an empty range means
	// there is no log and not that the read is still out.
	costs      map[cost.Range][]cost.Spend
	costLoaded bool

	// lastRead and lastCost are when the accounts and the logs were last
	// asked for, and refreshing and costRefreshing tell that a read is still
	// out. A pane shown again waits out the rest of the interval from the
	// last read, so that switching tabs does not read each time.
	lastRead, lastCost time.Time
	refreshing         bool
	costRefreshing     bool

	running bool
	cancel  context.CancelFunc
	wake    chan struct{}
	// retry tells the loop that a read it cancelled has ended, so that it
	// looks again at what is due: the timers were set while that read was
	// still out and its start was dropped.
	retry chan struct{}

	renderMu sync.Mutex
	work     sync.WaitGroup
}

// pane is one pane of the plugin: the size it draws at, whether it is on
// screen to read again, and what its user chose: the range of the cost and
// the accounts whose details are open.
type pane struct {
	id      string
	size    protocol.Size
	visible bool
	rng     string
	open    map[string]bool
}

// paneState is what a pane keeps in its target.
type paneState struct {
	Range string   `json:"range"`
	Open  []string `json:"open"`
}

// serve runs the plugin on the host's connection until the host goes away.
func serve(src Source) error {
	p := newPlugin(src)
	err := sdk.Run(sdk.Handlers{
		Open:    p.open,
		Close:   p.close,
		Resize:  p.resize,
		Visible: p.setVisible,
		Event:   p.event,
	})
	p.closeAll()
	return err
}

func newPlugin(src Source) *plugin {
	return &plugin{
		src:        src,
		usageEvery: refreshEvery,
		costWait:   costEvery,
		clock:      time.Now,
		panes:      map[string]*pane{},
		reports:    src.Providers(),
		providers:  src.Providers(),
		costs:      map[cost.Range][]cost.Spend{},
		wake:       make(chan struct{}, 1),
		retry:      make(chan struct{}, 1),
	}
}

// open draws a pane for the first time, with the choices it kept. The
// accounts and the cost are read once a pane is visible, not when it opens.
func (p *plugin) open(c *sdk.Conn, e sdk.OpenEvent) {
	st := &pane{id: e.Pane, size: e.Size, rng: rangeToday, open: map[string]bool{}}
	var saved paneState
	if len(e.State) > 0 && json.Unmarshal(e.State, &saved) == nil {
		if validRange(saved.Range) {
			st.rng = saved.Range
		}
		for _, id := range saved.Open {
			st.open[id] = true
		}
	}
	p.mu.Lock()
	p.conn = c
	if c != nil {
		p.save = c.SaveState
	}
	p.panes[e.Pane] = st
	p.mu.Unlock()
	p.render(e.Pane)
}

// close forgets a pane, and stops reading once no pane is visible again.
func (p *plugin) close(_ *sdk.Conn, id string) {
	p.mu.Lock()
	delete(p.panes, id)
	cancel := p.stopLocked()
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// resize records a pane's size and redraws it if it crossed the line between
// two forms.
func (p *plugin) resize(_ *sdk.Conn, id string, size protocol.Size) {
	p.mu.Lock()
	st := p.panes[id]
	if st == nil {
		p.mu.Unlock()
		return
	}
	was := layoutFor(st.size)
	st.size = size
	now := layoutFor(size)
	p.mu.Unlock()
	if was != now {
		p.render(id)
	}
}

// setVisible starts reading when a pane comes on screen, and stops once the
// last visible pane goes away: a hidden pane makes no request.
func (p *plugin) setVisible(_ *sdk.Conn, id string, visible bool) {
	p.mu.Lock()
	st := p.panes[id]
	if st == nil {
		p.mu.Unlock()
		return
	}
	st.visible = visible
	if visible && !p.running {
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.running = true
		p.work.Add(1)
		go func() {
			defer p.work.Done()
			p.loop(ctx)
		}()
	}
	cancel := p.stopLocked()
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if visible {
		p.render(id)
	}
}

// stopLocked stops the loop when it is running and no pane is visible. It
// returns the cancel to call off the lock.
func (p *plugin) stopLocked() context.CancelFunc {
	if !p.running || p.anyVisibleLocked() {
		return nil
	}
	cancel := p.cancel
	p.cancel = nil
	p.running = false
	return cancel
}

func (p *plugin) anyVisibleLocked() bool {
	for _, st := range p.panes {
		if st.visible {
			return true
		}
	}
	return false
}

// event handles a pane's controls: the refresh button reads the accounts and
// the logs now, the range control changes what the cost card shows, and a
// section's button opens or closes its details. A choice is saved with the
// pane.
func (p *plugin) event(_ *sdk.Conn, e protocol.EventParams) {
	switch {
	case e.Kind == protocol.KindPress && e.Node == "usage-refresh":
		select {
		case p.wake <- struct{}{}:
		default:
		}
	case e.Kind == protocol.KindSelect && e.Node == "usage-range":
		key, _ := e.Value.(string)
		if validRange(key) {
			p.choose(e.Pane, func(st *pane) { st.rng = key })
		}
	case e.Kind == protocol.KindPress && strings.HasPrefix(e.Node, "toggle-"):
		id := strings.TrimPrefix(e.Node, "toggle-")
		p.choose(e.Pane, func(st *pane) {
			if st.open[id] {
				delete(st.open, id)
			} else {
				st.open[id] = true
			}
		})
	}
}

// choose changes a pane's choices, saves them, and draws the pane alone: the
// range and the details are not shared. The cost of another range is already
// in memory, so the pane does not wait for a read.
func (p *plugin) choose(id string, change func(*pane)) {
	p.mu.Lock()
	st := p.panes[id]
	if st == nil {
		p.mu.Unlock()
		return
	}
	change(st)
	state := paneState{Range: st.rng, Open: make([]string, 0, len(st.open))}
	for open := range st.open {
		state.Open = append(state.Open, open)
	}
	save := p.save
	p.mu.Unlock()
	sort.Strings(state.Open)
	if save != nil {
		if err := save(id, state); err != nil && !errors.Is(err, sdk.ErrClosed) {
			fmt.Fprintln(os.Stderr, "the usage plugin:", err)
		}
	}
	p.render(id)
}

// loop reads while at least one pane is visible: the accounts every two
// minutes, the logs every five and at the first midnight after a read, and
// both at once when the refresh button asks for them. A pane is hidden
// whenever a frame does not draw it, so a loop that starts again keeps the
// intervals from the last reads; only the button reads sooner. The host
// cancels the loop when the last visible pane goes away or the plugin
// closes.
func (p *plugin) loop(ctx context.Context) {
	usageTimer := time.NewTimer(p.usageWait())
	costTimer := time.NewTimer(p.costDue())
	defer usageTimer.Stop()
	defer costTimer.Stop()

	p.showCached()
	for {
		select {
		case <-ctx.Done():
			return
		case <-usageTimer.C:
			p.startRefresh(ctx)
			usageTimer.Reset(p.usageEvery)
		case <-costTimer.C:
			p.startCostRefresh(ctx)
			costTimer.Reset(p.costDue())
		case <-p.retry:
			resetTimer(usageTimer, p.usageWait())
			resetTimer(costTimer, p.costDue())
		case <-p.wake:
			p.startRefresh(ctx)
			p.startCostRefresh(ctx)
			usageTimer.Reset(p.usageEvery)
			costTimer.Reset(p.costDue())
		}
	}
}

// resetTimer sets a timer that may have fired, or be waiting unread, to go
// off in d.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// readEnded tells the loop that a read it cancelled has ended.
func (p *plugin) readEnded() {
	select {
	case p.retry <- struct{}{}:
	default:
	}
}

// usageWait is how long a loop that starts waits before it reads the
// accounts: nothing when they were never read or usageEvery has passed, and
// the rest of usageEvery otherwise.
func (p *plugin) usageWait() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastRead.IsZero() {
		return 0
	}
	return max(p.usageEvery-p.clock().Sub(p.lastRead), 0)
}

// costDue is how long until the logs are read again: nothing when they were
// never read, otherwise the rest of the interval, cut short at the first
// local midnight after the last read, because the days shift there.
func (p *plugin) costDue() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastCost.IsZero() {
		return 0
	}
	now := p.clock()
	next := p.lastCost.Add(p.costWait)
	y, m, d := p.lastCost.Local().Date()
	if midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.Local); midnight.Before(next) {
		next = midnight
	}
	return max(next.Sub(now), 0)
}

// showCached draws what the logs said on an earlier run, which is in memory
// once the reader has loaded its cache, before the first scan ends.
func (p *plugin) showCached() {
	p.mu.Lock()
	loaded := p.costLoaded
	p.mu.Unlock()
	if loaded {
		return
	}
	spends := p.src.Spends(p.clock())
	p.mu.Lock()
	if !p.costLoaded && hasSpend(spends) {
		p.costs, p.costLoaded = spends, true
	}
	p.mu.Unlock()
	p.renderAll()
}

// hasSpend tells whether any range has a provider.
func hasSpend(spends map[cost.Range][]cost.Spend) bool {
	for _, s := range spends {
		if len(s) > 0 {
			return true
		}
	}
	return false
}

// startRefresh reads the accounts on a goroutine of its own, so that a slow
// provider does not hold up the logs. One read is out at a time; a tick or a
// press while it is out is dropped. Only the loop calls it, and the loop's
// own count in work keeps closeAll waiting for the read too.
func (p *plugin) startRefresh(ctx context.Context) {
	p.mu.Lock()
	if p.refreshing || ctx.Err() != nil || !p.anyVisibleLocked() {
		p.mu.Unlock()
		return
	}
	p.refreshing = true
	p.lastRead = p.clock()
	previous := p.reports
	p.loading = true
	p.work.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.work.Done()
		p.refreshUsage(ctx, previous)
	}()
}

// refreshUsage reads the accounts off the lock, then draws what it read. A
// read that finishes after the panes went away is dropped, and the foot stops
// saying it is checking.
func (p *plugin) refreshUsage(ctx context.Context, previous []accountusage.Report) {
	p.renderAll()
	installed := p.src.Installed()
	reports := p.src.Refresh(ctx, previous)

	p.mu.Lock()
	p.refreshing, p.loading = false, false
	cancelled := ctx.Err() != nil
	if cancelled {
		// The read did not finish: the accounts are due again at once.
		p.lastRead = time.Time{}
	} else {
		p.reports, p.installed = reports, installed
	}
	p.mu.Unlock()
	p.renderAll()
	if cancelled {
		p.readEnded()
	}
}

// startCostRefresh reads the logs on a goroutine of its own, like the
// accounts: the first read is slow. A read that the loop cancels is not
// counted, so the next one starts at once; the reader keeps what it did.
func (p *plugin) startCostRefresh(ctx context.Context) {
	p.mu.Lock()
	if p.costRefreshing || ctx.Err() != nil || !p.anyVisibleLocked() {
		p.mu.Unlock()
		return
	}
	p.costRefreshing = true
	now := p.clock()
	p.lastCost = now
	p.work.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.work.Done()
		err := p.src.RefreshCost(ctx, now)
		var spends map[cost.Range][]cost.Spend
		if err == nil && ctx.Err() == nil {
			spends = p.src.Spends(now)
		}
		p.mu.Lock()
		p.costRefreshing = false
		switch {
		case ctx.Err() != nil:
			p.lastCost = time.Time{}
		case spends != nil:
			p.costs, p.costLoaded = spends, true
		}
		p.mu.Unlock()
		p.renderAll()
		if ctx.Err() != nil {
			p.readEnded()
		}
	}()
}

// closeAll stops the loop and the reads it started, waits for them, and
// saves what the cost reader learned, before the process exits.
func (p *plugin) closeAll() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.running = false
	p.conn = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.work.Wait()
	if err := p.src.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "the usage plugin:", err)
	}
}

// renderAll redraws the panes on screen, because the accounts and the
// cost are shared. A hidden pane is drawn when it is shown again.
func (p *plugin) renderAll() {
	for _, id := range p.visiblePanes() {
		p.render(id)
	}
}

// visiblePanes is the panes a shared read draws: the ones on screen.
func (p *plugin) visiblePanes() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.panes))
	for id, st := range p.panes {
		if st.visible {
			ids = append(ids, id)
		}
	}
	return ids
}

// render draws one pane. The connection writes one message at a time, so the
// frames of the handler loop and a read that just finished take their turn
// under renderMu.
// The view is read inside renderMu too, so that a frame read earlier is never
// sent after a later one.
func (p *plugin) render(id string) {
	p.renderMu.Lock()
	defer p.renderMu.Unlock()
	v := p.snapshot(id)
	if v == nil {
		return
	}
	p.mu.Lock()
	c := p.conn
	p.mu.Unlock()
	if c == nil {
		return
	}
	err := c.Render(id, widgetTree(v, p.clock()))
	if err != nil && !errors.Is(err, sdk.ErrClosed) {
		fmt.Fprintln(os.Stderr, "the usage plugin:", err)
	}
}

// view is what one frame draws: a pane's size and choices, and the shared
// accounts and cost.
type view struct {
	size       protocol.Size
	reports    []accountusage.Report
	providers  []accountusage.Report
	loading    bool
	costs      map[cost.Range][]cost.Spend
	costLoaded bool
	rng        string
	open       map[string]bool
}

func (p *plugin) snapshot(id string) *view {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.panes[id]
	if st == nil {
		return nil
	}
	return &view{
		size:       st.size,
		reports:    filterReports(p.reports, p.installed),
		providers:  p.providers,
		loading:    p.loading,
		costs:      p.costs,
		costLoaded: p.costLoaded,
		rng:        st.rng,
		open:       maps.Clone(st.open),
	}
}
