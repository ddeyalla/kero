package usage

// The usage plugin: the account allowances of the coding agents on this Mac,
// and this Mac's CPU and memory, as a pane of their own. The plugin has no
// network code of its own; it reads the accounts through internal/usage and
// samples the machine through internal/resources, and it refreshes only
// while a pane of it is on screen.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
	"kero/internal/resources"
	accountusage "kero/internal/usage"
)

func init() {
	registry.RegisterRunner("usage", func() error { return serve(newSource()) })
}

const (
	// refreshEvery is how often a visible pane reads the accounts again, the
	// same two minutes the Usage tab keeps.
	refreshEvery = 2 * time.Minute

	// sampleEvery is how often a visible pane samples the CPU and the memory,
	// the same two seconds the Usage tab keeps.
	sampleEvery = 2 * time.Second

	// compactWidth and compactHeight are where a pane stops being its large
	// self: the same widget draws one row for each account, or the full card
	// with the resets and the machine sample.
	compactWidth  = 480
	compactHeight = 320
)

// plugin is the usage plugin's process: one pane or a few, and the accounts
// and the machine they share.
type plugin struct {
	src                     Source
	usageEvery, sampleEvery time.Duration
	clock                   func() time.Time

	mu        sync.Mutex
	conn      *sdk.Conn
	panes     map[string]*pane
	reports   []accountusage.Report
	installed []string
	sample    resources.Sample
	sampleErr string
	loading   bool

	// lastRead is when the accounts were last asked for, and refreshing
	// tells that a read is still out. A pane shown again waits out the rest
	// of usageEvery from lastRead, so that switching tabs does not read the
	// accounts each time.
	lastRead   time.Time
	refreshing bool

	running bool
	cancel  context.CancelFunc
	wake    chan struct{}

	renderMu sync.Mutex
	work     sync.WaitGroup
}

// pane is one pane of the plugin: the size it draws at, and whether it is on
// screen to read again.
type pane struct {
	id      string
	size    protocol.Size
	visible bool
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
		src:         src,
		usageEvery:  refreshEvery,
		sampleEvery: sampleEvery,
		clock:       time.Now,
		panes:       map[string]*pane{},
		reports:     src.Providers(),
		wake:        make(chan struct{}, 1),
	}
}

// open draws a pane for the first time. The accounts and the sample are read
// once a pane is visible, not when it opens.
func (p *plugin) open(c *sdk.Conn, e sdk.OpenEvent) {
	p.mu.Lock()
	p.conn = c
	p.panes[e.Pane] = &pane{id: e.Pane, size: e.Size}
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
// the short form and the large one.
func (p *plugin) resize(_ *sdk.Conn, id string, size protocol.Size) {
	p.mu.Lock()
	st := p.panes[id]
	if st == nil {
		p.mu.Unlock()
		return
	}
	was := compact(st.size)
	st.size = size
	now := compact(size)
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

// event refreshes the accounts and the machine when the pane's refresh button
// is pressed.
func (p *plugin) event(_ *sdk.Conn, e protocol.EventParams) {
	if e.Kind != protocol.KindPress || e.Node != "usage-refresh" {
		return
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// loop reads while at least one pane is visible: the accounts every two
// minutes, the machine every two seconds, and both at once when the refresh
// button asks for them. A pane is hidden whenever a frame does not draw it,
// so a loop that starts again keeps the accounts' two minutes from the last
// read; only the button reads them sooner. The machine's sampler starts
// afresh, so the first CPU reading is not an average over the hidden span.
// The host cancels the loop when the last visible pane goes away or the
// plugin closes.
func (p *plugin) loop(ctx context.Context) {
	p.src.ResetSample()
	usageTimer := time.NewTimer(p.usageWait())
	sampleTick := time.NewTicker(p.sampleEvery)
	defer usageTimer.Stop()
	defer sampleTick.Stop()

	p.sampleOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-usageTimer.C:
			p.startRefresh(ctx)
			usageTimer.Reset(p.usageEvery)
		case <-sampleTick.C:
			p.sampleOnce(ctx)
		case <-p.wake:
			p.startRefresh(ctx)
			p.sampleOnce(ctx)
			usageTimer.Reset(p.usageEvery)
		}
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

// startRefresh reads the accounts on a goroutine of its own, so that a slow
// provider does not hold up the machine's bars. One read is out at a time;
// a tick or a press while it is out is dropped. Only the loop calls it, and
// the loop's own count in work keeps closeAll waiting for the read too.
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
// read that finishes after the panes went away is dropped, and the head stops
// saying it is checking.
func (p *plugin) refreshUsage(ctx context.Context, previous []accountusage.Report) {
	p.renderAll()
	installed := p.src.Installed()
	reports := p.src.Refresh(ctx, previous)

	p.mu.Lock()
	p.refreshing, p.loading = false, false
	if ctx.Err() == nil {
		p.reports, p.installed = reports, installed
	}
	p.mu.Unlock()
	p.renderAll()
}

// sampleOnce samples the machine off the lock, then draws what it read.
func (p *plugin) sampleOnce(ctx context.Context) {
	p.mu.Lock()
	if ctx.Err() != nil || !p.anyVisibleLocked() {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	sample, err := p.src.Sample()
	if ctx.Err() != nil {
		return
	}

	p.mu.Lock()
	p.sample, p.sampleErr = sample, ""
	if err != nil {
		p.sampleErr = err.Error()
	}
	p.mu.Unlock()
	p.renderAll()
}

// closeAll stops the loop and waits for it before the process exits.
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
}

// renderAll redraws the panes on screen, because the accounts and the
// machine are shared. A hidden pane is drawn when it is shown again.
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
	err := c.Render(id, widgetTree(v, time.Now()))
	if err != nil && !errors.Is(err, sdk.ErrClosed) {
		fmt.Fprintln(os.Stderr, "the usage plugin:", err)
	}
}

// view is what one frame draws: a pane's size, and the shared accounts and
// machine sample.
type view struct {
	size      protocol.Size
	reports   []accountusage.Report
	loading   bool
	sample    resources.Sample
	sampleErr string
}

func (p *plugin) snapshot(id string) *view {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.panes[id]
	if st == nil {
		return nil
	}
	return &view{
		size:      st.size,
		reports:   filterReports(p.reports, p.installed),
		loading:   p.loading,
		sample:    p.sample,
		sampleErr: p.sampleErr,
	}
}
