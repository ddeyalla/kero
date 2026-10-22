package linear

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
)

// fakeDaemon is kerod for the plugin: it records the calls, answers them
// from a table, and pushes states.
type fakeDaemon struct {
	mu       sync.Mutex
	fn       func(*proto.State, string)
	calls    []call
	handlers map[string]func(params any, out any) error
	closed   bool
}

type call struct {
	method string
	params any
}

func (f *fakeDaemon) Watch(fn func(*proto.State, string)) {
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()
}

func (f *fakeDaemon) Call(method string, params, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, call{method, params})
	h := f.handlers[method]
	f.mu.Unlock()
	if h != nil {
		return h(params, out)
	}
	return nil
}

func (f *fakeDaemon) Close() { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

func (f *fakeDaemon) push(st *proto.State) {
	f.mu.Lock()
	fn := f.fn
	f.mu.Unlock()
	fn(st, "")
}

func (f *fakeDaemon) down(why string) {
	f.mu.Lock()
	fn := f.fn
	f.mu.Unlock()
	fn(nil, why)
}

func (f *fakeDaemon) called(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// sink records the trees the plugin draws.
type sink struct {
	mu    sync.Mutex
	trees map[string][]protocol.Tree
}

func (s *sink) send(pane string, tree protocol.Tree) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trees == nil {
		s.trees = map[string][]protocol.Tree{}
	}
	s.trees[pane] = append(s.trees[pane], tree)
	return nil
}

func (s *sink) count(pane string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.trees[pane])
}

func (s *sink) last(t *testing.T, pane string) protocol.Tree {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.trees[pane]
	if len(list) == 0 {
		t.Fatalf("pane %s was never drawn", pane)
	}
	return list[len(list)-1]
}

// saves records what a pane kept.
type saves struct {
	mu    sync.Mutex
	saved []paneState
}

func (s *saves) save(_ string, state any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, state.(paneState))
	return nil
}

func (s *saves) all() []paneState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.saved)
}

type rig struct {
	p     *plugin
	d     *fakeDaemon
	out   *sink
	saved *saves
	urls  []string
	mu    sync.Mutex
}

func newRig(t *testing.T) *rig {
	t.Helper()
	d := &fakeDaemon{handlers: map[string]func(any, any) error{}}
	r := &rig{d: d, out: &sink{}, saved: &saves{}}
	p := newPlugin(d)
	p.send = r.out.send
	p.save = r.saved.save
	p.saveDelay = 10 * time.Millisecond
	p.clock = func() time.Time { return epoch }
	p.openURL = func(link string) error { r.mu.Lock(); r.urls = append(r.urls, link); r.mu.Unlock(); return nil }
	r.p = p
	t.Cleanup(p.shutdown)
	return r
}

func (r *rig) open(pane string, state string) {
	var raw json.RawMessage
	if state != "" {
		raw = json.RawMessage(state)
	}
	r.p.open(nil, sdk.OpenEvent{Pane: pane, State: raw, Size: protocol.Size{Width: 600, Height: 500}})
}

func (r *rig) press(pane, node string) {
	r.p.event(nil, protocol.EventParams{Pane: pane, Node: node, Kind: protocol.KindPress})
}

func (r *rig) pick(pane, node, value string) {
	r.p.event(nil, protocol.EventParams{Pane: pane, Node: node, Kind: protocol.KindSelect, Value: value})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDrawsOnlyWhenVisibleAndRelevant(t *testing.T) {
	r := newRig(t)
	s := workspace()
	st := stateOfSnap(s, session("s1", "pr", "p1"))
	r.open("p1", "")
	first := r.out.count("p1")
	if find(r.out.last(t, "p1"), "empty-down") == nil {
		t.Fatal("before the daemon is heard from the pane says it is connecting")
	}

	// Hidden: the state is kept, not drawn.
	r.d.push(st)
	if r.out.count("p1") != first {
		t.Fatal("a hidden pane was drawn")
	}
	r.p.setVisible(nil, "p1", true)
	if find(r.out.last(t, "p1"), "sec:current") == nil {
		t.Fatalf("a pane shown draws what came while it was hidden: %v", ids(r.out.last(t, "p1")))
	}
	shown := r.out.count("p1")

	// Ticks of a terminal, a progress, a pane target (the plugin's own
	// SaveState echo): the same snapshot, so nothing is drawn.
	for i := 0; i < 20; i++ {
		tick := stateOfSnap(s, session("s1", "pr", "p1"))
		tick.Sessions[0].Activity, tick.Sessions[0].Since = int64(i), int64(i)
		tick.Sessions[0].Panes[0].Target = `{"plugin":"linear","state":{"query":"` + string(rune('a'+i)) + `"}}`
		tick.Sessions[0].Panes[0].Activity = int64(i)
		tick.Linear.Issues = slices.Clone(tick.Linear.Issues)
		tick.Linear.Issues[0].Progress = i
		r.d.push(tick)
	}
	if got := r.out.count("p1"); got != shown {
		t.Fatalf("status ticks drew the pane %d times", got-shown)
	}

	// A change it shows draws it.
	changed := stateOfSnap(s, session("s1", "pr", "p1"))
	changed.Linear.Issues = slices.Clone(changed.Linear.Issues)
	changed.Linear.Issues[3].Title = "A new title"
	r.d.push(changed)
	if r.out.count("p1") != shown+1 || !strings.Contains(treeText(r.out.last(t, "p1")), "A new title") {
		t.Fatalf("a change of an issue should draw once, drew %d", r.out.count("p1")-shown)
	}

	// Hidden again, a change waits for the pane to come back.
	r.p.setVisible(nil, "p1", false)
	n := r.out.count("p1")
	changed2 := stateOfSnap(s, session("s1", "pr", "p1"))
	changed2.Linear.Issues = slices.Clone(changed2.Linear.Issues)
	changed2.Linear.Issues[3].Title = "Newer still"
	r.d.push(changed2)
	if r.out.count("p1") != n {
		t.Fatal("a hidden pane was drawn for a change")
	}
	r.p.setVisible(nil, "p1", true)
	if !strings.Contains(treeText(r.out.last(t, "p1")), "Newer still") {
		t.Fatal("the pane did not catch up when shown")
	}
}

func TestDaemonGoingAwayAndBack(t *testing.T) {
	r := newRig(t)
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(workspace()))
	if find(r.out.last(t, "p1"), "sec:current") == nil {
		t.Fatal("no issues")
	}
	r.d.down("kerod went away")
	if n := find(r.out.last(t, "p1"), "empty-down"); n == nil || !strings.Contains(n.Detail, "went away") {
		t.Fatalf("a lost daemon should say so: %+v", n)
	}
	r.d.push(stateOfSnap(workspace()))
	if find(r.out.last(t, "p1"), "sec:current") == nil {
		t.Fatal("the pane did not come back with the daemon")
	}
}

func TestCollapseRoundTripThroughSaveState(t *testing.T) {
	r := newRig(t)
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(workspace()))
	r.press("p1", "sec:current")
	r.press("p1", "grp:todo:"+slug("Web"))
	r.press("p1", "kid:id-1")
	r.press("p1", "kid:id-1") // and back
	r.press("p1", "more:done")
	r.pick("p1", "mode", "board")
	r.pick("p1", "f-assignee", "me")
	r.press("p1", "f-canceled")
	for _, ch := range []string{"p", "pa", "par"} {
		r.p.event(nil, protocol.EventParams{Pane: "p1", Node: "q", Kind: protocol.KindChange, Value: ch})
	}
	waitFor(t, "the state to be kept", func() bool {
		all := r.saved.all()
		return len(all) > 0 && all[len(all)-1].Query == "par"
	})
	if n := len(r.saved.all()); n > 2 {
		t.Fatalf("the typing and clicking saved %d times; saves are debounced", n)
	}
	got := r.saved.all()[len(r.saved.all())-1]
	if got.Mode != modeBoard || got.Filters.Assignee != assigneeMe || !got.Filters.Canceled || !got.DoneAll ||
		!slices.Contains(got.Collapsed, "sec:current") || !slices.Contains(got.Collapsed, "grp:todo:"+slug("Web")) || slices.Contains(got.Collapsed, "kid:id-1") {
		t.Fatalf("kept %+v", got)
	}

	// A new process opens the pane with it.
	raw, _ := json.Marshal(got)
	q := newRig(t)
	q.open("p9", string(raw))
	q.p.setVisible(nil, "p9", true)
	q.d.push(stateOfSnap(workspace()))
	in, _ := q.p.view("p9")
	if in.st.Mode != modeBoard || in.st.Query != "par" || !slices.Equal(in.st.Collapsed, got.Collapsed) {
		t.Fatalf("restored %+v", in.st)
	}

	// The list restores folded.
	got.Mode = modeList
	got.Query = ""
	got.Filters = filters{}
	raw, _ = json.Marshal(got)
	q2 := newRig(t)
	q2.open("p9", string(raw))
	q2.p.setVisible(nil, "p9", true)
	q2.d.push(stateOfSnap(workspace()))
	tree := q2.out.last(t, "p9")
	if find(tree, "sec:current") == nil || find(tree, "i:id-1") != nil {
		t.Fatal("the folded section came back open")
	}
}

func TestLegacyTasksStateAndGarbage(t *testing.T) {
	r := newRig(t)
	r.open("p1", `{"mode":"list","scope":"project","hideDone":true}`)
	in, _ := r.p.view("p1")
	if in.st.Scope != scopeProjectName || !slices.Equal(in.st.Collapsed, []string{"sec:done"}) || in.st.HideDone != nil {
		t.Fatalf("the Tasks pane's state became %+v", in.st)
	}
	r.open("p2", `{"mode":"sideways","scope":"x"`)
	in, _ = r.p.view("p2")
	if in.st.Mode != modeList || in.st.Scope != scopeAll {
		t.Fatalf("a broken state became %+v", in.st)
	}
	r.open("p3", `{"mode":"sideways","scope":"x"}`)
	in, _ = r.p.view("p3")
	if in.st.Mode != modeList || in.st.Scope != scopeAll {
		t.Fatalf("a strange state became %+v", in.st)
	}
}

func TestFocusReordersCurrent(t *testing.T) {
	r := newRig(t)
	s := snap(
		issue(1, "started", withSession("s1", "one")),
		issue(2, "started"),
	)
	st := stateOfSnap(s, session("focus", "pr", "p9"), session("s1", "pr", "p8"))
	st.Sessions[0].Panes[0].TaskID = "BRU-2"
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(st)
	if rows := rowIDs(r.out.last(t, "p1")); rows[0] != "id-1" {
		t.Fatalf("unfocused %v", rows)
	}
	r.p.setFocus(nil, "focus")
	if rows := rowIDs(r.out.last(t, "p1")); rows[0] != "id-2" {
		t.Fatalf("focused on a session working on BRU-2: %v", rows)
	}
	n := r.out.count("p1")
	r.p.setFocus(nil, "focus")
	if r.out.count("p1") != n {
		t.Fatal("the same focus drew again")
	}
	r.p.setFocus(nil, "")
	if rows := rowIDs(r.out.last(t, "p1")); rows[0] != "id-1" {
		t.Fatalf("focus lost %v", rows)
	}
}

func TestFilterEventsNarrowTheTree(t *testing.T) {
	r := newRig(t)
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(workspace()))
	r.pick("p1", "f-assignee", "me")
	if rows := rowIDs(r.out.last(t, "p1")); !slices.Equal(rows, []string{"id-1"}) {
		t.Fatalf("assigned to me: %v", rows)
	}
	r.pick("p1", "f-assignee", "any")
	r.pick("p1", "f-project", "p:"+slug("Web"))
	if rows := rowIDs(r.out.last(t, "p1")); !slices.Equal(rows, []string{"id-4", "id-6"}) {
		t.Fatalf("in project Web: %v", rows)
	}
	r.pick("p1", "f-project", "all")
	r.pick("p1", "f-priority", "pr:1")
	if rows := rowIDs(r.out.last(t, "p1")); !slices.Equal(rows, []string{"id-4"}) {
		t.Fatalf("urgent: %v", rows)
	}
	r.pick("p1", "f-priority", "any")
	r.pick("p1", "mode", "board")
	if find(r.out.last(t, "p1"), "board") == nil {
		t.Fatal("no board")
	}
	r.pick("p1", "mode", "nonsense")
	if find(r.out.last(t, "p1"), "board") == nil {
		t.Fatal("a bad mode changed the view")
	}
}

func openIssueRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.d.handlers["linear.issue"] = func(params any, out any) error {
		*(out.(*proto.LinearIssueDetail)) = *fullDetail()
		return nil
	}
	s := workspace()
	st := stateOfSnap(s, session("s1", "pr", "p9"), session("s2", "pr", "p1"))
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(st)
	r.press("p1", "i:id-1")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })
	return r
}

func TestOpenIssueReadsAndShowsItAndGoesBack(t *testing.T) {
	r := openIssueRig(t)
	calls := r.d.called("linear.issue")
	if len(calls) != 1 || calls[0].params.(proto.LinearParams).Issue != "id-1" {
		t.Fatalf("calls %+v", calls)
	}
	// The open issue is kept with the pane.
	waitFor(t, "open to be saved", func() bool {
		all := r.saved.all()
		return len(all) > 0 && all[len(all)-1].Open == "id-1"
	})
	r.press("p1", "d-back")
	if find(r.out.last(t, "p1"), "sec:current") == nil || find(r.out.last(t, "p1"), "d-desc") != nil {
		t.Fatal("back did not return to the list")
	}
	waitFor(t, "close to be saved", func() bool {
		all := r.saved.all()
		return all[len(all)-1].Open == ""
	})

	// A pane restored with an issue open reads it when shown.
	q := newRig(t)
	q.d.handlers["linear.issue"] = func(params any, out any) error {
		*(out.(*proto.LinearIssueDetail)) = *fullDetail()
		return nil
	}
	q.open("p1", `{"open":"id-1"}`)
	q.p.setVisible(nil, "p1", true)
	q.d.push(stateOfSnap(workspace()))
	waitFor(t, "the restored detail", func() bool { return find(q.out.last(t, "p1"), "d-desc") != nil })
}

func TestDetailIsReadAgainWhenTheBoardSaysItChanged(t *testing.T) {
	r := openIssueRig(t)
	st := stateOfSnap(workspace(), session("s1", "pr", "p9"), session("s2", "pr", "p1"))
	r.d.push(st) // nothing changed
	st.Linear.Issues = slices.Clone(st.Linear.Issues)
	st.Linear.Issues[0].UpdatedAt++
	st.Linear.Issues[0].State, st.Linear.Issues[0].StateType = "In Review", "started"
	r.d.push(st)
	waitFor(t, "a second read", func() bool { return len(r.d.called("linear.issue")) == 2 })
	time.Sleep(30 * time.Millisecond)
	if n := len(r.d.called("linear.issue")); n != 2 {
		t.Fatalf("%d reads", n)
	}
}

func TestDetailFailureOffersRetry(t *testing.T) {
	r := newRig(t)
	fail := true
	r.d.handlers["linear.issue"] = func(params any, out any) error {
		if fail {
			return errors.New("linear is down")
		}
		*(out.(*proto.LinearIssueDetail)) = *fullDetail()
		return nil
	}
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(workspace()))
	r.press("p1", "i:id-1")
	waitFor(t, "the error", func() bool { return find(r.out.last(t, "p1"), "d-err") != nil })
	fail = false
	r.press("p1", "d-err")
	waitFor(t, "the retry", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })
}

func TestDetailWrites(t *testing.T) {
	r := openIssueRig(t)

	// The status picker moves the issue through the daemon.
	r.pick("p1", "d-status", "s:"+slug("In Review"))
	waitFor(t, "setState", func() bool { return len(r.d.called("linear.setState")) == 1 })
	if got := r.d.called("linear.setState")[0].params.(proto.LinearParams); got.Issue != "id-1" || got.State != "In Review" {
		t.Fatalf("setState %+v", got)
	}
	if find(r.out.last(t, "p1"), "d-status").Label != "In Review" {
		t.Fatal("the move did not show while it was on its way")
	}
	r.pick("p1", "d-status", "s:"+slug("In Review")) // where it is going: nothing to send
	r.pick("p1", "d-status", "s:nonsense")
	time.Sleep(20 * time.Millisecond)
	if n := len(r.d.called("linear.setState")); n != 1 {
		t.Fatalf("%d moves", n)
	}

	// Open in Linear.
	r.press("p1", "d-ext")
	waitFor(t, "the browser", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.urls) == 1 })
	if r.urls[0] != "https://linear.app/team/issue/BRU-1" {
		t.Fatalf("opened %q", r.urls[0])
	}
	r.press("p1", "att0")
	waitFor(t, "the attachment", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.urls) == 2 })
	r.p.event(nil, protocol.EventParams{Pane: "p1", Node: "d-desc", Kind: protocol.KindPress, Value: "file:///etc/passwd"})
	r.p.event(nil, protocol.EventParams{Pane: "p1", Node: "d-desc", Kind: protocol.KindPress, Value: "https://example.com/x"})
	waitFor(t, "the link", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.urls) == 3 })
	time.Sleep(20 * time.Millisecond)
	r.mu.Lock()
	if len(r.urls) != 3 || r.urls[2] != "https://example.com/x" {
		t.Fatalf("a local file may not open: %v", r.urls)
	}
	r.mu.Unlock()

	// Open the running session; resume the ended run in a new one.
	r.press("p1", "run0-open")
	waitFor(t, "select", func() bool { return len(r.d.called("select")) == 1 })
	if got := r.d.called("select")[0].params.(proto.FocusParams); got.Session != "s1" {
		t.Fatalf("select %+v", got)
	}
	r.d.handlers["create"] = func(params any, out any) error {
		*(out.(*proto.Session)) = proto.Session{ID: "new-session"}
		return nil
	}
	r.press("p1", "run1-resume")
	waitFor(t, "create", func() bool { return len(r.d.called("create")) == 1 })
	got := r.d.called("create")[0].params.(proto.CreateParams)
	if got.Task != "BRU-1" || got.Dir != "/y" || !slices.Equal(got.Command, []string{"codex", "resume", "def456"}) || got.Name != "bru-1-codex" {
		t.Fatalf("resume created %+v", got)
	}
	waitFor(t, "the new session shown", func() bool { return len(r.d.called("select")) == 2 })
	if r.d.called("select")[1].params.(proto.FocusParams).Session != "new-session" {
		t.Fatal("the new session was not selected")
	}

	// Unlink the session working on it.
	r.press("p1", "s-unlink")
	waitFor(t, "unlink", func() bool { return len(r.d.called("session.link")) == 1 })
	if l := r.d.called("session.link")[0].params.(proto.LinkParams); l.Session != "s1" || l.Task != "" {
		t.Fatalf("unlink %+v", l)
	}
}

func TestStartAndLinkAnIssueWithNoSession(t *testing.T) {
	r := newRig(t)
	r.d.handlers["linear.issue"] = func(params any, out any) error {
		*(out.(*proto.LinearIssueDetail)) = proto.LinearIssueDetail{LinearIssue: issue(5, "backlog")}
		return nil
	}
	r.d.handlers["create"] = func(params any, out any) error {
		*(out.(*proto.Session)) = proto.Session{ID: "fresh"}
		return nil
	}
	s := workspace()
	s.Projects = []proto.Project{{ID: "pr", Name: "bare"}}
	st := stateOfSnap(s, session("s2", "pr", "p1"))
	st.Projects = s.Projects
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(st)
	r.press("p1", "i:id-5")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "s-start") != nil })

	r.press("p1", "s-start")
	waitFor(t, "create", func() bool { return len(r.d.called("create")) == 1 })
	got := r.d.called("create")[0].params.(proto.CreateParams)
	if got.Task != "BRU-5" || got.Project != "pr" || got.Name != "bru-5" {
		t.Fatalf("start task created %+v", got)
	}

	r.pick("p1", "s-link", "ss:s2")
	waitFor(t, "link", func() bool { return len(r.d.called("session.link")) == 1 })
	if l := r.d.called("session.link")[0].params.(proto.LinkParams); l.Session != "s2" || l.Task != "BRU-5" {
		t.Fatalf("link %+v", l)
	}
}

func TestWriteFailureShowsANotice(t *testing.T) {
	r := openIssueRig(t)
	r.d.handlers["linear.setState"] = func(any, any) error { return errors.New("not allowed") }
	r.pick("p1", "d-status", "s:"+slug("In Review"))
	waitFor(t, "the notice", func() bool {
		n := find(r.out.last(t, "p1"), "notice")
		return n != nil && n.Text == "not allowed"
	})
	if find(r.out.last(t, "p1"), "d-status").Label != "In Progress" {
		t.Fatal("a refused move must not keep showing")
	}
}

func TestRefreshAndEmptyActions(t *testing.T) {
	r := newRig(t)
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(workspace()))
	r.press("p1", "refresh")
	r.p.command(nil, "refresh")
	waitFor(t, "two refreshes", func() bool { return len(r.d.called("linear.refresh")) == 2 })

	r.p.event(nil, protocol.EventParams{Pane: "p1", Node: "q", Kind: protocol.KindChange, Value: "zzzz"})
	r.pick("p1", "f-assignee", "me")
	if find(r.out.last(t, "p1"), "empty-none") == nil {
		t.Fatal("nothing should match")
	}
	r.press("p1", "empty-none")
	in, _ := r.p.view("p1")
	if in.st.Query != "" || in.st.Filters.active() {
		t.Fatalf("Clear Filters left %+v", in.st)
	}

	// The scope prompt: show everything.
	s := workspace()
	s.Projects = []proto.Project{{ID: "pr", Name: "bare"}}
	st := stateOfSnap(s, session("s1", "pr", "p2"))
	st.Projects = s.Projects
	r.open("p2", `{"scope":"project"}`)
	r.p.setVisible(nil, "p2", true)
	r.d.push(st)
	if find(r.out.last(t, "p2"), "empty-scope") == nil {
		t.Fatal("no prompt")
	}
	r.press("p2", "empty-scope")
	if find(r.out.last(t, "p2"), "empty-scope") != nil || find(r.out.last(t, "p2"), "sec:current") == nil {
		t.Fatal("Show all did not show everything")
	}
}

func TestSettings(t *testing.T) {
	r := newRig(t)
	r.open("p1", `{"mode":"board"}`)
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(snap(issue(1, "started"))))
	if find(r.out.last(t, "p1"), "col:backlog") == nil {
		t.Fatal("empty columns show by default")
	}
	r.p.setSettings(nil, map[string]string{"empty-columns": "hide", "density": "compact"})
	tree := r.out.last(t, "p1")
	if find(tree, "col:backlog") != nil || find(tree, "c:id-1") == nil {
		t.Fatal("the setting should hide empty columns")
	}
	n := r.out.count("p1")
	r.p.setSettings(nil, map[string]string{"empty-columns": "hide", "density": "compact"})
	if r.out.count("p1") != n {
		t.Fatal("the same settings drew again")
	}
}

func TestCloseForgetsThePane(t *testing.T) {
	r := newRig(t)
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.press("p1", "mode")
	r.p.close(nil, "p1")
	r.d.push(stateOfSnap(workspace()))
	n := r.out.count("p1")
	r.pick("p1", "mode", "board")
	if r.out.count("p1") != n {
		t.Fatal("a closed pane was drawn")
	}
}
