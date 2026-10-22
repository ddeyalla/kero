// Package linear is the Linear plugin: the issues of the team, as a list or
// a board, with the issue open in the pane, as a pane of its own.
//
// The plugin has no Linear code and never sees the API key. It is a client
// of kerod, which holds the key and reads the workspace: the plugin
// subscribes to kerod's state, draws it, and writes through kerod's calls
// (move an issue, link a session, start one). It draws only while one of
// its panes is on screen, and only when something it shows changed: the
// state kerod sends on every tick of a terminal does not draw it again.
package linear

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
)

func init() {
	registry.RegisterRunner("linear", func() error { return serve(newDaemon()) })
}

const (
	// saveDelay is how long a pane waits after the last change of its
	// choices before it keeps them: typing in the search does not save on
	// every letter.
	saveDelay = 400 * time.Millisecond

	// movingFor is how long a move asked for stands in for the board's
	// state, before the board, read since, has the last word.
	movingFor = 20 * time.Second
)

// plugin is the Linear plugin's process: the panes it draws, and the one
// view of the workspace they share.
type plugin struct {
	d         Daemon
	clock     func() time.Time
	saveDelay time.Duration

	// save keeps a pane's state in its target. It is the connection's
	// SaveState once a pane opens; a test replaces it.
	save func(pane string, state any) error
	// openURL opens a link in the browser; the connection's, replaced by a
	// test.
	openURL func(link string) error
	// copyText puts text on the clipboard; the connection's, replaced by a
	// test.
	copyText func(text string) error
	// labels are the colors of the labels the pane has read with an issue,
	// by name, which the cards use: the board's issues carry names only.
	labels map[string]string
	// send draws a pane's tree. It is the connection's Render; a test
	// replaces it to read the trees.
	send func(pane string, tree protocol.Tree) error

	mu       sync.Mutex
	conn     *sdk.Conn
	panes    map[string]*pane
	watching bool

	// latest is the last state kerod sent; dirty tells that it was not
	// worked on, as no pane was on screen to draw it.
	latest *proto.State
	dirty  bool
	snap   *snapshot
	hash   uint64

	// moves are the moves the user asked for, by issue id, which the board
	// shows at once and until kerod's state has them or they fail.
	moves map[string]pendingMove

	connected bool
	down      string
	focus     string
	settings  settings

	renderMu sync.Mutex
	work     sync.WaitGroup
}

// pendingMove is a move of an issue to a state, as asked for.
type pendingMove struct {
	state string
	at    time.Time
}

// pane is one pane of the plugin: what it chose, and the issue it has open.
type pane struct {
	id      string
	visible bool
	st      paneState

	// The open issue, as read. detailKey is what the board said of the
	// issue as it was read, so that a change of it reads it again.
	detail    *proto.LinearIssueDetail
	detailFor string
	detailKey string
	detailErr string
	detailSeq uint64

	moving   string
	movingAt time.Time
	notice   string

	// sel is the issue the keys have selected, order the issues in the
	// order the last tree showed them, and width the pane's width.
	sel   string
	order []string
	width int

	saveTimer *time.Timer
}

// serve runs the plugin on the host's connection until the host goes away.
func serve(d Daemon) error {
	p := newPlugin(d)
	err := sdk.Run(sdk.Handlers{
		Open:     p.open,
		Close:    p.close,
		Visible:  p.setVisible,
		Event:    p.event,
		Command:  p.command,
		Settings: p.setSettings,
		Focus:    p.setFocus,
		Resize:   p.resize,
	})
	p.shutdown()
	return err
}

func newPlugin(d Daemon) *plugin {
	return &plugin{d: d, clock: time.Now, saveDelay: saveDelay, panes: map[string]*pane{}, snap: &snapshot{}}
}

// shutdown ends the watch of the daemon and waits for the calls in flight.
func (p *plugin) shutdown() {
	p.d.Close()
	p.mu.Lock()
	for _, pn := range p.panes {
		if pn.saveTimer != nil {
			pn.saveTimer.Stop()
		}
	}
	p.mu.Unlock()
	p.work.Wait()
}

// open draws a pane for the first time, with the choices it kept.
func (p *plugin) open(c *sdk.Conn, e sdk.OpenEvent) {
	pn := &pane{id: e.Pane, st: paneState{Mode: modeList, Scope: scopeAll}, width: e.Size.Width}
	if len(e.State) > 0 {
		var saved paneState
		if json.Unmarshal(e.State, &saved) == nil {
			pn.st = saved
			if pn.st.Mode != modeBoard {
				pn.st.Mode = modeList
			}
			if pn.st.Scope != scopeProjectName {
				pn.st.Scope = scopeAll
			}
			// The Tasks pane this replaces folded the finished work away.
			if saved.HideDone != nil {
				if *saved.HideDone && saved.Collapsed == nil {
					pn.st.Collapsed = []string{"sec:" + secDone}
				}
				pn.st.HideDone = nil
			}
		}
	}
	p.mu.Lock()
	p.conn = c
	if c != nil {
		p.save = c.SaveState
		p.openURL = c.OpenURL
		p.copyText = c.Copy
	}
	p.panes[e.Pane] = pn
	start := !p.watching
	p.watching = true
	p.mu.Unlock()
	if start {
		p.d.Watch(p.onState)
	}
	p.render(e.Pane)
	p.loadDetailIfOpen(e.Pane)
}

// resize takes a pane's new width. The open issue stands beside the issues
// from a width on, so a pane that crosses it is drawn again.
func (p *plugin) resize(_ *sdk.Conn, id string, size protocol.Size) {
	p.mu.Lock()
	pn := p.panes[id]
	if pn == nil {
		p.mu.Unlock()
		return
	}
	crossed := (pn.width >= wideWidth) != (size.Width >= wideWidth) && pn.st.Open != "" && pn.visible
	pn.width = size.Width
	p.mu.Unlock()
	if crossed {
		p.render(id)
	}
}

// close forgets a pane.
func (p *plugin) close(_ *sdk.Conn, id string) {
	p.mu.Lock()
	if pn := p.panes[id]; pn != nil && pn.saveTimer != nil {
		pn.saveTimer.Stop()
	}
	delete(p.panes, id)
	p.mu.Unlock()
}

// setVisible draws a pane that came on screen with what changed while it
// was hidden.
func (p *plugin) setVisible(_ *sdk.Conn, id string, visible bool) {
	p.mu.Lock()
	pn := p.panes[id]
	if pn == nil {
		p.mu.Unlock()
		return
	}
	pn.visible = visible
	p.mu.Unlock()
	if !visible {
		return
	}
	p.catchUp()
	p.render(id)
	p.loadDetailIfOpen(id)
}

// setFocus records the session the app has selected, which orders the
// Current section, and draws the panes on screen again.
func (p *plugin) setFocus(_ *sdk.Conn, session string) {
	p.mu.Lock()
	if p.focus == session {
		p.mu.Unlock()
		return
	}
	p.focus = session
	p.mu.Unlock()
	p.renderVisible()
}

// setSettings reads the plugin's settings, which the host sends when a
// value changes.
func (p *plugin) setSettings(_ *sdk.Conn, values map[string]string) {
	next := settings{
		HideEmptyColumns: isOff(values["empty-columns"]),
		Compact:          strings.EqualFold(strings.TrimSpace(values["density"]), "compact"),
	}
	p.mu.Lock()
	changed := p.settings != next
	p.settings = next
	p.mu.Unlock()
	if changed {
		p.renderVisible()
	}
}

// isOff reads a setting that says no.
func isOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hide", "no", "off", "false", "0":
		return true
	}
	return false
}

// command runs one of the plugin's commands.
func (p *plugin) command(_ *sdk.Conn, id string) {
	if id == "refresh" {
		p.refresh()
	}
}

// refresh asks kerod to read the workspace now.
func (p *plugin) refresh() {
	p.async(func() {
		if err := p.d.Call("linear.refresh", nil, nil); err != nil {
			p.noticeAll(err.Error())
		}
	})
}

// async runs a call of the daemon off the handler, which must not wait on it.
func (p *plugin) async(fn func()) {
	p.work.Add(1)
	go func() {
		defer p.work.Done()
		fn()
	}()
}

// onState takes what kerod sent: its state, or that it is out of reach.
func (p *plugin) onState(st *proto.State, down string) {
	p.mu.Lock()
	if st == nil {
		changed := p.connected || p.down != down
		p.connected, p.down = false, down
		p.mu.Unlock()
		if changed {
			p.renderVisible()
		}
		return
	}
	reconnected := !p.connected
	p.connected, p.down = true, ""
	p.latest = st
	if !p.anyVisibleLocked() {
		p.dirty = true
		p.mu.Unlock()
		return
	}
	changed := p.absorbLocked() || reconnected
	p.mu.Unlock()
	if changed {
		p.renderVisible()
		p.reloadDetails()
	}
}

func (p *plugin) anyVisibleLocked() bool {
	for _, pn := range p.panes {
		if pn.visible {
			return true
		}
	}
	return false
}

// catchUp works on the state that arrived while no pane was on screen.
func (p *plugin) catchUp() {
	p.mu.Lock()
	if p.dirty {
		p.dirty = false
		p.absorbLocked()
	}
	p.mu.Unlock()
}

// absorbLocked reduces the latest state to what the pane draws, and tells
// whether that differs from what was drawn last. A state that differs only
// in what ticks (activity, progress, a status) is the same snapshot.
func (p *plugin) absorbLocked() bool {
	if p.latest == nil {
		return false
	}
	p.dirty = false
	// The fingerprint reads the state where it stands, so that a broadcast
	// that changed nothing the pane draws costs no copy of the issues.
	sum := fingerprint(p.latest)
	if sum == p.hash && p.snap.Has == (p.latest.Linear != nil) {
		return false
	}
	next := snapshotOf(p.latest)
	p.hash = sum
	p.snap = &next
	p.settleMovesLocked()
	p.pruneFoldsLocked()
	return true
}

// settleMovesLocked forgets the moves the board has caught up with, and
// those that waited too long.
func (p *plugin) settleMovesLocked() {
	for id, m := range p.moves {
		if is := p.snap.issue(id); is == nil || is.State == m.state || p.clock().Sub(m.at) >= movingFor {
			delete(p.moves, id)
		}
	}
}

// pruneFoldsLocked drops the fold keys of issues and groups the workspace no
// longer has, so that a pane's target does not grow for ever. It acts only on
// a real snapshot with issues in it: an empty or failed read says nothing of
// what exists.
func (p *plugin) pruneFoldsLocked() {
	snap := p.snap
	if !snap.Has || snap.Status != proto.LinearOK || len(snap.Issues) == 0 {
		return
	}
	ids := make(map[string]bool, len(snap.Issues))
	projects := map[string]bool{}
	for _, is := range snap.Issues {
		ids[is.ID] = true
		projects[slug(is.Project)] = true
	}
	for _, pn := range p.panes {
		kept := slices.DeleteFunc(slices.Clone(pn.st.Collapsed), func(k string) bool {
			switch {
			case strings.HasPrefix(k, "kid:"):
				return !ids[k[len("kid:"):]]
			case strings.HasPrefix(k, "grp:"):
				i := strings.LastIndexByte(k, ':')
				return !projects[k[i+1:]]
			}
			return false
		})
		if len(kept) != len(pn.st.Collapsed) {
			pn.st.Collapsed = kept
			p.persistLocked(pn)
		}
	}
}

// withMoves shows the snapshot with the moves the user asked for made: the
// issues in their new states.
func withMoves(snap *snapshot, moves map[string]pendingMove) *snapshot {
	if len(moves) == 0 {
		return snap
	}
	next := *snap
	next.Issues = slices.Clone(snap.Issues)
	for i, is := range next.Issues {
		m, ok := moves[is.ID]
		if !ok {
			continue
		}
		if st, ok := snap.stateByName(m.state); ok {
			next.Issues[i].State, next.Issues[i].StateType = st.Name, st.Type
		}
	}
	return &next
}

// view reads what a tree of the pane is drawn from.
func (p *plugin) view(id string) (viewInput, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pn := p.panes[id]
	if pn == nil {
		return viewInput{}, false
	}
	in := viewInput{
		pane:      id,
		snap:      withMoves(p.snap, p.activeMovesLocked()),
		st:        pn.st,
		now:       p.clock(),
		settings:  p.settings,
		connected: p.connected,
		down:      p.down,
		notice:    pn.notice,
		width:     pn.width,
		sel:       pn.sel,
	}
	if len(p.labels) > 0 {
		in.labelColors = maps.Clone(p.labels)
	}
	in.st.Collapsed = slices.Clone(pn.st.Collapsed)
	if se := p.snap.session(p.focus); se != nil {
		in.focus = focusInfo{Session: se.ID, Tasks: se.Tasks}
	}
	if pn.st.Open != "" {
		if pn.detailFor == pn.st.Open {
			in.detail = pn.detail
		}
		in.detailErr = pn.detailErr
		if pn.moving != "" && p.clock().Sub(pn.movingAt) < movingFor {
			in.moving = pn.moving
		}
	}
	return in, true
}

// activeMovesLocked are the moves still waiting for the board.
func (p *plugin) activeMovesLocked() map[string]pendingMove {
	var out map[string]pendingMove
	for id, m := range p.moves {
		if p.clock().Sub(m.at) >= movingFor {
			continue
		}
		if out == nil {
			out = map[string]pendingMove{}
		}
		out[id] = m
	}
	return out
}

// render draws one pane.
func (p *plugin) render(id string) {
	in, ok := p.view(id)
	if !ok {
		return
	}
	tree, order := buildTreeOrder(in)
	p.mu.Lock()
	if pn := p.panes[id]; pn != nil {
		pn.order = order
	}
	p.mu.Unlock()
	p.renderMu.Lock()
	defer p.renderMu.Unlock()
	p.mu.Lock()
	send := p.send
	if send == nil && p.conn != nil {
		send = p.conn.Render
	}
	p.mu.Unlock()
	if send == nil {
		return
	}
	if err := send(id, tree); err != nil && err != sdk.ErrClosed {
		fmt.Fprintln(os.Stderr, "the linear plugin:", err)
	}
}

// renderVisible draws the panes on screen.
func (p *plugin) renderVisible() {
	p.mu.Lock()
	var ids []string
	for id, pn := range p.panes {
		if pn.visible {
			ids = append(ids, id)
		}
	}
	p.mu.Unlock()
	slices.Sort(ids)
	for _, id := range ids {
		p.render(id)
	}
}

// noticeAll tells every pane of something that went wrong.
func (p *plugin) noticeAll(msg string) {
	p.mu.Lock()
	for _, pn := range p.panes {
		pn.notice = msg
	}
	p.mu.Unlock()
	p.renderVisible()
}

// notice tells a pane of something that went wrong; "" clears it.
func (p *plugin) notice(id, msg string) {
	p.mu.Lock()
	if pn := p.panes[id]; pn != nil {
		pn.notice = msg
	}
	p.mu.Unlock()
	p.render(id)
}

// persist keeps the pane's choices in its target once they rest.
func (p *plugin) persistLocked(pn *pane) {
	id := pn.id
	if pn.saveTimer == nil {
		pn.saveTimer = time.AfterFunc(p.saveDelay, func() { p.flush(id) })
		return
	}
	pn.saveTimer.Reset(p.saveDelay)
}

func (p *plugin) flush(id string) {
	p.mu.Lock()
	pn := p.panes[id]
	if pn == nil {
		p.mu.Unlock()
		return
	}
	state := pn.st
	state.HideDone = nil
	state.Collapsed = slices.Clone(pn.st.Collapsed)
	slices.Sort(state.Collapsed)
	save := p.save
	p.mu.Unlock()
	if save == nil {
		return
	}
	if err := save(id, state); err != nil && err != sdk.ErrClosed {
		fmt.Fprintln(os.Stderr, "the linear plugin:", err)
	}
}

// --- the open issue ---

// liveKey is what of an issue the detail depends on that the board keeps
// current: when it changed, where it is, who works on it.
func liveKey(is *proto.LinearIssue) string {
	if is == nil {
		return ""
	}
	return strings.Join([]string{strconv.FormatInt(is.UpdatedAt, 10), is.State, is.Session, strconv.Itoa(is.Sessions)}, "|")
}

// loadDetailIfOpen reads the issue a pane has open, when it has one that
// is not read.
func (p *plugin) loadDetailIfOpen(id string) {
	p.mu.Lock()
	pn := p.panes[id]
	if pn == nil || pn.st.Open == "" || pn.detailFor == pn.st.Open && pn.detail != nil {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.loadDetail(id)
}

// reloadDetails reads the open issues again that the board says changed,
// rather than on a timer, which would spend Linear's quota on issues that
// sit still.
func (p *plugin) reloadDetails() {
	p.mu.Lock()
	var ids []string
	for id, pn := range p.panes {
		if pn.st.Open == "" || pn.detailFor != pn.st.Open || !pn.visible {
			continue
		}
		if liveKey(p.snap.issue(pn.st.Open)) != pn.detailKey {
			ids = append(ids, id)
		}
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.loadDetail(id)
	}
}

// loadDetail reads the issue a pane has open, from kerod, and draws it.
func (p *plugin) loadDetail(id string) {
	p.mu.Lock()
	pn := p.panes[id]
	if pn == nil || pn.st.Open == "" {
		p.mu.Unlock()
		return
	}
	pn.detailSeq++
	seq, issue := pn.detailSeq, pn.st.Open
	key := liveKey(p.snap.issue(issue))
	if pn.detailFor != issue {
		pn.detail, pn.detailFor = nil, issue
	}
	pn.detailErr = ""
	p.mu.Unlock()
	p.async(func() {
		var d proto.LinearIssueDetail
		err := p.d.Call("linear.issue", proto.LinearParams{Issue: issue}, &d)
		p.mu.Lock()
		pn := p.panes[id]
		if pn == nil || pn.detailSeq != seq || pn.st.Open != issue {
			p.mu.Unlock()
			return
		}
		if err != nil {
			pn.detail, pn.detailErr = nil, err.Error()
		} else {
			pn.detail, pn.detailKey = &d, key
			for _, l := range d.LabelColors {
				if protocol.ValidHex(l.Color) {
					if p.labels == nil {
						p.labels = map[string]string{}
					}
					p.labels[l.Name] = l.Color
				}
			}
			if pn.moving != "" && d.State == pn.moving {
				pn.moving = ""
			}
		}
		p.mu.Unlock()
		p.render(id)
	})
}

// --- the user's choices ---

// event handles a pane's controls.
func (p *plugin) event(_ *sdk.Conn, e protocol.EventParams) {
	p.mu.Lock()
	pn := p.panes[e.Pane]
	if pn == nil {
		p.mu.Unlock()
		return
	}
	pn.notice = ""
	do := p.dispatchLocked(pn, e)
	p.mu.Unlock()
	if do.render {
		p.render(e.Pane)
	}
	if do.load {
		p.loadDetail(e.Pane)
	}
	if do.run != nil {
		p.async(do.run)
	}
}

// outcome is what a handled event asks to be done after the lock is let go.
type outcome struct {
	render bool
	load   bool
	run    func()
}

// choose gives the pane's persisted choices a change and draws it.
func (p *plugin) choose(pn *pane, change func(st *paneState)) outcome {
	change(&pn.st)
	p.persistLocked(pn)
	return outcome{render: true}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// toggle folds or unfolds a section, a group or an issue.
func toggle(list []string, key string) []string {
	if i := slices.Index(list, key); i >= 0 {
		return slices.Delete(slices.Clone(list), i, i+1)
	}
	return append(slices.Clone(list), key)
}

func (p *plugin) dispatchLocked(pn *pane, e protocol.EventParams) outcome {
	id, kind := e.Node, e.Kind
	snap := p.snap
	switch {
	case id == "q" && (kind == protocol.KindChange || kind == protocol.KindSubmit):
		q := str(e.Value)
		return p.choose(pn, func(st *paneState) { st.Query = q })

	case id == "mode" && kind == protocol.KindSelect:
		mode := str(e.Value)
		if mode != modeList && mode != modeBoard {
			return outcome{}
		}
		return p.choose(pn, func(st *paneState) { st.Mode = mode })

	case id == "refresh" && kind == protocol.KindPress, id == "empty-unread" && kind == protocol.KindPress:
		return outcome{run: p.refresh}

	case id == "f-canceled" && kind == protocol.KindPress:
		return p.choose(pn, func(st *paneState) { st.Filters.Canceled = !st.Filters.Canceled })

	case strings.HasPrefix(id, "f-") && kind == protocol.KindSelect:
		return p.selectFilter(pn, id, str(e.Value))

	case id == "empty-scope" && kind == protocol.KindPress:
		return p.choose(pn, func(st *paneState) { st.Scope = scopeAll })

	case id == "empty-none" && kind == protocol.KindPress:
		if pn.st.Filters.active() || strings.TrimSpace(pn.st.Query) != "" {
			return p.choose(pn, func(st *paneState) { st.Filters, st.Query = filters{}, "" })
		}
		return outcome{run: p.refresh}

	case kind == protocol.KindKey && id == "keys":
		return p.keyLocked(pn, str(e.Value))

	case kind == protocol.KindContext && (strings.HasPrefix(id, "i:") || strings.HasPrefix(id, "c:")):
		return p.contextLocked(pn, id[2:], str(e.Value))

	case kind == protocol.KindPress && (strings.HasPrefix(id, "sec:") || strings.HasPrefix(id, "grp:")):
		return p.choose(pn, func(st *paneState) { st.Collapsed = toggle(st.Collapsed, id) })

	case kind == protocol.KindPress && strings.HasPrefix(id, "kid:"):
		return p.choose(pn, func(st *paneState) { st.Collapsed = toggle(st.Collapsed, id) })

	case kind == protocol.KindPress && id == "more:"+secDone:
		return p.choose(pn, func(st *paneState) { st.DoneAll = true })

	case kind == protocol.KindPress && (strings.HasPrefix(id, "i:") || strings.HasPrefix(id, "c:") || strings.HasPrefix(id, "sub:")):
		return p.openIssueLocked(pn, id[strings.IndexByte(id, ':')+1:])

	case kind == protocol.KindPress && id == "rel-parent", kind == protocol.KindPress && strings.HasPrefix(id, "rel") && len(id) > 3 && id[3] != '-':
		if pn.detail == nil {
			return outcome{}
		}
		var ref string
		if id == "rel-parent" {
			ref = pn.detail.Parent
		} else if n, err := strconv.Atoi(id[3:]); err == nil && n >= 0 && n < len(pn.detail.Relations) {
			ref = pn.detail.Relations[n].Identifier
		}
		if is := snap.issue(ref); is != nil {
			return p.openIssueLocked(pn, is.ID)
		}
		return outcome{}
	}
	if pn.st.Open != "" {
		return p.detailEventLocked(pn, e)
	}
	return outcome{}
}

// selectFilter handles a choice in one of the filter menus.
func (p *plugin) selectFilter(pn *pane, menu, item string) outcome {
	snap := p.snap
	return p.choose(pn, func(st *paneState) {
		f := &st.Filters
		switch menu {
		case "f-assignee":
			switch {
			case item == "any":
				f.Assignee = assigneeAny
			case item == "me":
				f.Assignee = assigneeMe
			case strings.HasPrefix(item, "u:"):
				f.Assignee = item[2:]
			}
		case "f-project":
			f.Project = ""
			for _, name := range projectNames(snap) {
				if item == "p:"+slug(name) {
					f.Project = name
				}
			}
		case "f-label":
			f.Label = ""
			for _, l := range labelsOf(snap) {
				if item == "l:"+slug(l) {
					f.Label = l
				}
			}
		case "f-priority":
			f.Priority = ""
			if strings.HasPrefix(item, "pr:") {
				if n, err := strconv.Atoi(item[3:]); err == nil && n >= 0 && n <= 4 {
					f.Priority = itoa(n)
				}
			}
		}
	})
}

// openIssueLocked opens an issue in the pane's place and reads it.
func (p *plugin) openIssueLocked(pn *pane, ref string) outcome {
	is := p.snap.issue(ref)
	if is == nil {
		return outcome{}
	}
	pn.st.Open, pn.sel = is.ID, is.ID
	pn.detail, pn.detailFor, pn.detailErr, pn.moving = nil, "", "", ""
	p.persistLocked(pn)
	return outcome{render: true, load: true}
}

// detailEventLocked handles the controls of the open issue.
func (p *plugin) detailEventLocked(pn *pane, e protocol.EventParams) outcome {
	id, kind := e.Node, e.Kind
	snap := p.snap
	live := snap.issue(pn.st.Open)
	d := pn.detail
	paneID := pn.id
	open := func(link string) outcome { return p.openLinkOutcome(paneID, link) }
	// A link in the description or a comment, when the host sends the
	// press instead of opening it.
	if kind == protocol.KindPress && (id == "d-desc" || strings.HasPrefix(id, "cm") && strings.HasSuffix(id, "-b")) {
		return open(str(e.Value))
	}
	switch {
	case id == "d-back" && kind == protocol.KindPress:
		return p.closeDetailLocked(pn)

	case (id == "d-prev" || id == "d-next") && kind == protocol.KindPress:
		if id == "d-prev" {
			return p.stepLocked(pn, -1)
		}
		return p.stepLocked(pn, 1)

	case id == "d-copy-id" && kind == protocol.KindPress:
		switch {
		case live != nil:
			return p.copyOutcome(paneID, live.Identifier)
		case d != nil:
			return p.copyOutcome(paneID, d.Identifier)
		}

	case id == "d-copy-link" && kind == protocol.KindPress:
		switch {
		case live != nil:
			return p.copyOutcome(paneID, live.URL)
		case d != nil:
			return p.copyOutcome(paneID, d.URL)
		}

	case id == "d-copy-branch" && kind == protocol.KindPress && d != nil:
		return p.copyOutcome(paneID, d.BranchName)

	case strings.HasPrefix(id, "run") && strings.HasSuffix(id, "-copy") && kind == protocol.KindPress && d != nil:
		if n, err := strconv.Atoi(strings.TrimSuffix(id[3:], "-copy")); err == nil && n >= 0 && n < len(d.Runs) {
			return p.copyOutcome(paneID, d.Runs[n].ID)
		}

	case (id == "d-err" || id == "d-gone") && kind == protocol.KindPress:
		return outcome{render: true, load: true}

	case id == "d-ext" && kind == protocol.KindPress:
		switch {
		case live != nil:
			return open(live.URL)
		case d != nil:
			return open(d.URL)
		}

	case id == "d-status" && kind == protocol.KindSelect:
		if live == nil {
			return outcome{}
		}
		var name string
		for _, s := range snap.States {
			if "s:"+slug(s.Name) == str(e.Value) {
				name = s.Name
			}
		}
		return p.moveLocked(pn, live, name)

	case id == "s-open" && kind == protocol.KindPress && live != nil:
		return p.selectSession(live.Session, paneID)

	case id == "s-unlink" && kind == protocol.KindPress && live != nil:
		session := live.Session
		return outcome{run: func() { p.link(paneID, session, "") }}

	case id == "s-link" && kind == protocol.KindSelect && live != nil && strings.HasPrefix(str(e.Value), "ss:"):
		session, ident := str(e.Value)[3:], live.Identifier
		return outcome{run: func() { p.link(paneID, session, ident) }}

	case id == "s-start" && kind == protocol.KindPress && live != nil:
		return p.startOutcome(pn, live.Identifier)

	case strings.HasPrefix(id, "run") && kind == protocol.KindPress && d != nil && live != nil:
		return p.runEventLocked(pn, id, d, live)

	case strings.HasPrefix(id, "att") && kind == protocol.KindPress && d != nil:
		if n, err := strconv.Atoi(id[3:]); err == nil && n >= 0 && n < len(d.Attachments) {
			return open(d.Attachments[n].URL)
		}
	}
	return outcome{}
}

// runEventLocked handles Open and Resume on a run of the issue.
func (p *plugin) runEventLocked(pn *pane, id string, d *proto.LinearIssueDetail, live *proto.LinearIssue) outcome {
	rest := strings.TrimPrefix(id, "run")
	i := strings.IndexByte(rest, '-')
	if i < 0 {
		return outcome{}
	}
	n, err := strconv.Atoi(rest[:i])
	if err != nil || n < 0 || n >= len(d.Runs) {
		return outcome{}
	}
	r, paneID := d.Runs[n], pn.id
	switch rest[i+1:] {
	case "open":
		return p.selectSession(r.Session, paneID)
	case "resume":
		cmd := proto.ResumeCommand(r.Agent, r.ID)
		if cmd == nil {
			return outcome{}
		}
		project := p.projectOfLocked(paneID)
		if se := p.snap.session(r.Session); se != nil {
			project = se.Project
		}
		params := proto.CreateParams{Dir: r.Dir, Command: cmd, Task: live.Identifier,
			Name: strings.ToLower(live.Identifier) + "-" + r.Agent, Project: project}
		return outcome{run: func() { p.create(paneID, params) }}
	}
	return outcome{}
}

// openLinkOutcome opens a web link in the browser.
func (p *plugin) openLinkOutcome(paneID, link string) outcome {
	if !safeURL(link) {
		return outcome{}
	}
	fn := p.openURL
	return outcome{run: func() {
		if fn == nil {
			return
		}
		if err := fn(link); err != nil {
			p.notice(paneID, err.Error())
		}
	}}
}

// copyOutcome puts text on the clipboard; the app says it was copied.
func (p *plugin) copyOutcome(paneID, text string) outcome {
	if text == "" {
		return outcome{}
	}
	fn := p.copyText
	return outcome{run: func() {
		if fn == nil {
			return
		}
		if err := fn(text); err != nil {
			p.notice(paneID, err.Error())
		}
	}}
}

// closeDetailLocked closes the open issue and shows the issues again.
func (p *plugin) closeDetailLocked(pn *pane) outcome {
	pn.st.Open = ""
	pn.detail, pn.detailFor, pn.detailErr, pn.moving = nil, "", "", ""
	pn.detailSeq++
	p.persistLocked(pn)
	return outcome{render: true}
}

// moveLocked asks to move an issue to the state with the name. The detail
// shows the state it was asked to move to, until the board has it.
func (p *plugin) moveLocked(pn *pane, live *proto.LinearIssue, name string) outcome {
	if name == "" {
		return outcome{}
	}
	if _, pending := p.moves[live.ID]; !pending && name == live.State {
		return outcome{}
	}
	paneID, issue := pn.id, live.ID
	if m, ok := p.moves[issue]; ok && m.state == name {
		return outcome{}
	}
	// The row moves to its new section at once, in every pane; it moves back
	// if kerod refuses.
	if p.moves == nil {
		p.moves = map[string]pendingMove{}
	}
	p.moves[issue] = pendingMove{state: name, at: p.clock()}
	if pn.st.Open == issue {
		pn.moving, pn.movingAt = name, p.clock()
	}
	return outcome{render: true, run: func() {
		err := p.d.Call("linear.setState", proto.LinearParams{Issue: issue, State: name}, nil)
		if err == nil {
			return
		}
		p.mu.Lock()
		if m, ok := p.moves[issue]; ok && m.state == name {
			delete(p.moves, issue)
		}
		if pn := p.panes[paneID]; pn != nil && pn.moving == name {
			pn.moving = ""
		}
		p.mu.Unlock()
		p.renderVisible()
		p.notice(paneID, err.Error())
	}}
}

// startOutcome starts a task on an issue in a new session.
func (p *plugin) startOutcome(pn *pane, ident string) outcome {
	paneID, project := pn.id, p.projectOfLocked(pn.id)
	return outcome{run: func() {
		p.create(paneID, proto.CreateParams{Task: ident, Name: strings.ToLower(ident), Project: project})
	}}
}

// contextLocked handles a choice in the menu of a right click on an issue.
func (p *plugin) contextLocked(pn *pane, ref, item string) outcome {
	is := p.snap.issue(ref)
	if is == nil {
		return outcome{}
	}
	switch {
	case item == "m-open":
		return p.openIssueLocked(pn, is.ID)
	case item == "m-ext":
		return p.openLinkOutcome(pn.id, is.URL)
	case item == "m-copy-id":
		return p.copyOutcome(pn.id, is.Identifier)
	case item == "m-copy-link":
		return p.copyOutcome(pn.id, is.URL)
	case item == "m-start":
		return p.startOutcome(pn, is.Identifier)
	case strings.HasPrefix(item, "mv:"):
		for _, s := range p.snap.States {
			if "mv:"+slug(s.Name) == item {
				return p.moveLocked(pn, is, s.Name)
			}
		}
	}
	return outcome{}
}

// keyLocked handles a key the pane asked to hear: J and K and the vertical
// arrows step through the issues, the sideways arrows too while one is open,
// Enter opens the selected one, Escape closes the open one.
func (p *plugin) keyLocked(pn *pane, key string) outcome {
	switch key {
	case "j", "down":
		return p.stepLocked(pn, 1)
	case "k", "up":
		return p.stepLocked(pn, -1)
	case "right":
		if pn.st.Open != "" {
			return p.stepLocked(pn, 1)
		}
	case "left":
		if pn.st.Open != "" {
			return p.stepLocked(pn, -1)
		}
	case "enter":
		if pn.st.Open == "" && pn.sel != "" {
			return p.openIssueLocked(pn, pn.sel)
		}
	case "escape":
		if pn.st.Open != "" {
			return p.closeDetailLocked(pn)
		}
	}
	return outcome{}
}

// stepLocked moves the selection, or the open issue, by one in the order the
// pane shows the issues. From nothing it takes the first or the last.
func (p *plugin) stepLocked(pn *pane, delta int) outcome {
	if len(pn.order) == 0 {
		return outcome{}
	}
	cur := pn.sel
	if pn.st.Open != "" {
		if is := p.snap.issue(pn.st.Open); is != nil {
			cur = is.ID
		}
	}
	next := slices.Index(pn.order, cur) + delta
	if slices.Index(pn.order, cur) < 0 {
		next = 0
		if delta < 0 {
			next = len(pn.order) - 1
		}
	}
	if next < 0 || next >= len(pn.order) {
		return outcome{}
	}
	if pn.st.Open != "" {
		return p.openIssueLocked(pn, pn.order[next])
	}
	pn.sel = pn.order[next]
	return outcome{render: true}
}

// projectOfLocked is the project of the session that holds the pane, "" for
// the daemon's first.
func (p *plugin) projectOfLocked(pane string) string {
	if se := p.snap.sessionOfPane(pane); se != nil {
		return se.Project
	}
	if se := p.snap.session(p.focus); se != nil {
		return se.Project
	}
	return ""
}

// selectSession shows a session in the app.
func (p *plugin) selectSession(session, pane string) outcome {
	if session == "" {
		return outcome{}
	}
	return outcome{run: func() {
		if err := p.d.Call("select", proto.FocusParams{Session: session}, nil); err != nil {
			p.notice(pane, err.Error())
		}
	}}
}

// link binds a session to an issue, or unbinds it when the issue is "", and
// reads the issue again.
func (p *plugin) link(pane, session, ident string) {
	err := p.d.Call("session.link", proto.LinkParams{Session: session, Task: ident}, nil)
	if err != nil {
		p.notice(pane, err.Error())
		return
	}
	p.loadDetail(pane)
}

// create starts a session on the issue, and shows it.
func (p *plugin) create(pane string, params proto.CreateParams) {
	var s proto.Session
	if err := p.d.Call("create", params, &s); err != nil {
		p.notice(pane, err.Error())
		return
	}
	if err := p.d.Call("select", proto.FocusParams{Session: s.ID}, nil); err != nil {
		p.notice(pane, err.Error())
	}
	p.loadDetail(pane)
}
