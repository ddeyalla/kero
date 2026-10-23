package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/client"
	"kero/internal/proto"
	"kero/locales"
)

func sidebarTestApp(t *testing.T) *App {
	a := testApp(t)
	a.conn = connReady
	a.st.Started = 100
	a.st.Projects = append(a.st.Projects, proto.Project{ID: "p2", Name: "second"})
	a.st.Sessions[0].Name = "first"
	for _, id := range []string{"s2", "s3", "s4"} {
		a.st.Sessions = append(a.st.Sessions, proto.Session{ID: id, Project: "p1", Name: id})
	}
	a.st.Sessions = append(a.st.Sessions, proto.Session{ID: "other", Project: "p2", Name: "other"})
	return a
}

// sidebarRPC records a real client request without starting a daemon or
// attaching terminals. Its socket and handshake belong only to this test.
func sidebarRPC(t *testing.T, a *App) <-chan proto.MoveParams {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sidebar-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "d.sock")
	t.Setenv("KERO_SOCKET", socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	moves := make(chan proto.MoveParams, 10)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec, enc := json.NewDecoder(conn), json.NewEncoder(conn)
		var hello proto.Hello
		if dec.Decode(&hello) != nil {
			return
		}
		if enc.Encode(proto.Welcome{Version: proto.Version}) != nil {
			return
		}
		for {
			var req struct {
				ID     int64
				Method string
				Params json.RawMessage
			}
			if dec.Decode(&req) != nil {
				return
			}
			if req.Method == "move" {
				var p proto.MoveParams
				if json.Unmarshal(req.Params, &p) != nil {
					return
				}
				moves <- p
			}
			if enc.Encode(proto.Message{ID: req.ID, Result: true}) != nil {
				return
			}
		}
	}()
	a.cl, err = client.Connect(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.cl.Close() })
	return moves
}

func wantSidebarMove(t *testing.T, moves <-chan proto.MoveParams, want proto.MoveParams) {
	t.Helper()
	select {
	case got := <-moves:
		if got != want {
			t.Fatalf("move = %+v, want %+v", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the UI did not send its move to the daemon")
	}
}

func TestSessionPinsOrderNavigationAndPersistence(t *testing.T) {
	a := sidebarTestApp(t)
	a.run("session.pin", map[string]string{"session": "s3"})
	a.run("session.pin", map[string]string{"session": "other"})
	want := []string{"s3", "s1", "s2", "s4", "other"}
	if got := a.visibleOrder(); !slices.Equal(got, want) {
		t.Fatalf("sidebar = %v, want %v", got, want)
	}
	var palette []string
	for _, item := range a.paletteItems() {
		if item.session != nil {
			palette = append(palette, item.session.ID)
		}
	}
	if !slices.Equal(palette, want) {
		t.Fatalf("palette = %v", palette)
	}
	a.selected = "s3"
	a.selectOffset(1)
	if a.selected != "s1" {
		t.Fatalf("next selected %s", a.selected)
	}
	a.prefs.Collapsed["p1"] = true
	if got := a.visibleOrder(); !slices.Equal(got, []string{"s1", "other"}) {
		t.Fatalf("collapsed order = %v", got)
	}
	a.flushPrefs()
	saved := readPrefs(t)
	if !saved.SessionPins["s3"] || !saved.SessionPins["other"] {
		t.Fatal("pins not saved")
	}
	a.prefs = prefs{}
	a.loadPrefs()
	if !a.prefs.SessionPins["s3"] {
		t.Fatal("saved pin did not load")
	}
	// The asynchronous writer must own its map.
	clone := clonePrefs(a.prefs)
	a.run("session.unpin", map[string]string{"session": "s3"})
	if !clone.SessionPins["s3"] {
		t.Fatal("prefs snapshot shares its pin map")
	}
	a.flushPrefs()
	a.prefs = prefs{}
	a.loadPrefs()
	if a.prefs.SessionPins["s3"] {
		t.Fatal("saved unpin did not load")
	}
}

// A negative asynchronous RPC assertion must keep listening while the
// client's sender runs. Use the positive assertion's same three-second
// deadline so an ordinary delayed send cannot escape this observation.
func wantNoSidebarMove(t *testing.T, moves <-chan proto.MoveParams) {
	t.Helper()
	select {
	case p := <-moves:
		t.Fatalf("unexpected sidebar move %+v", p)
	case <-time.After(3 * time.Second):
	}
}

func TestSessionPinsSurvivePartialRestoreAndPruneSeenDeletion(t *testing.T) {
	a := sidebarTestApp(t)
	a.setSessionPin("s3", true)
	a.loaded = false
	a.pruneSessionPins(proto.State{Started: 200})
	if !a.prefs.SessionPins["s3"] {
		t.Fatal("first partial state lost pin")
	}
	a.loaded = true
	a.pruneSessionPins(proto.State{Started: 200})
	if !a.prefs.SessionPins["s3"] {
		t.Fatal("new daemon lost pin")
	}
	// A session actually observed, then removed by this daemon, is pruned.
	st := a.st
	st.Sessions = slices.DeleteFunc(slices.Clone(st.Sessions), func(s proto.Session) bool { return s.ID == "s3" })
	a.pruneSessionPins(st)
	a.flushPrefs()
	if readPrefs(t).SessionPins["s3"] {
		t.Fatal("closed session pin remained on disk")
	}
}

func TestSessionMoveIndicesAndBoundaries(t *testing.T) {
	a := sidebarTestApp(t)
	a.setSessionPin("s2", true)
	a.setSessionPin("s4", true)
	for _, tc := range []struct {
		id, target string
		after      bool
		index      int
	}{
		{"s1", "s3", true, 2}, {"s3", "s1", false, 0}, {"s2", "s4", true, 3}, {"s4", "s2", false, 1},
	} {
		got, ok := a.sessionMove(tc.id, tc.target, tc.after)
		if !ok || got.Index != tc.index || got.Project != "" {
			t.Errorf("%+v: %+v, %v", tc, got, ok)
		}
	}
	for _, pair := range [][2]string{{"s1", "s2"}, {"s1", "other"}, {"s1", "s1"}, {"missing", "s1"}, {"s1", "missing"}} {
		if _, ok := a.sessionMove(pair[0], pair[1], true); ok {
			t.Errorf("invalid move %v accepted", pair)
		}
	}
	if a.sessionNeighbor("s2", -1) != "" || a.sessionNeighbor("s4", 1) != "" || a.sessionNeighbor("s1", -1) != "" {
		t.Fatal("menu movement crossed pin boundary")
	}
	a.conn = connFailed
	a.reorderSession("s1", "s3", true)
	if a.pendingToast != locales.SessionOrderUnavailable() {
		t.Fatal("offline reorder gave no feedback")
	}
}

func TestSessionMenuPinAndReorder(t *testing.T) {
	a := sidebarTestApp(t)
	moves := sidebarRPC(t, a)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if err := tt.RightClick("s3"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.SessionPin()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "sidebar-session-pinned")
	pinnedRow, _ := tt.Find("s3")
	unpinnedRow, _ := tt.Find("s2")
	if pinnedRow.Y >= unpinnedRow.Y {
		t.Fatal("pinned row did not move above unpinned rows")
	}
	if !a.prefs.SessionPins["s3"] || a.visibleOrder()[0] != "s3" {
		t.Fatal("menu did not pin first")
	}
	if err := tt.RightClick("s3"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), locales.SessionUnpin()) {
		t.Fatal("pinned menu offers no unpin")
	}
	if err := tt.ChooseMenuItem(locales.SessionUnpin()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.RightClick("s3"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.SessionMoveUp()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	wantSidebarMove(t, moves, proto.MoveParams{ID: "s3", Index: 1})
	// No speculative local success: the daemon has not broadcast a new state.
	if a.visibleOrder()[2] != "s3" {
		t.Fatal("order changed before daemon confirmed")
	}
	shot(t, tt, "sidebar-session-pins")
}

func TestSessionDragReordersWithinSection(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
			a := sidebarTestApp(t)
			moves := sidebarRPC(t, a)
			tt := ui.NewTester(a.view, 1100, 760)
			tt.Frame()
			src, _ := tt.Find("s4")
			dst, _ := tt.Find("s2")
			x, y := src.X+src.W/2, src.Y+src.H/2
			tx, ty := dst.X+dst.W/2, dst.Y
			if after {
				ty += dst.H
			}
			tt.Press(x, y)
			for i := 1; i <= 12; i++ {
				f := float32(i) / 12
				tt.Move(x+(tx-x)*f, y+(ty-y)*f)
				tt.Advance(16 * time.Millisecond)
				tt.Frame()
			}
			shot(t, tt, "sidebar-session-drop")
			tt.Release(tx, ty)
			tt.Frame()
			index := 1
			if after {
				index++
			}
			wantSidebarMove(t, moves, proto.MoveParams{ID: "s4", Index: index})
		})
	}
}

func TestSessionRemoteOrderActions(t *testing.T) {
	a := sidebarTestApp(t)
	moves := sidebarRPC(t, a)
	a.run("session.moveDown", map[string]string{"session": "s2"})
	wantSidebarMove(t, moves, proto.MoveParams{ID: "s2", Index: 2})
	a.run("session.moveUp", map[string]string{"session": "s3"})
	wantSidebarMove(t, moves, proto.MoveParams{ID: "s3", Index: 1})
	a.run("session.pin", map[string]string{"session": "gone"})
	if a.pendingToast != locales.SessionOrderMissing() {
		t.Fatal("unknown session silently accepted")
	}
}

func TestSessionPinKeepsProjectAndNewSessionsUnpinned(t *testing.T) {
	a := sidebarTestApp(t)
	a.setSessionPin("s3", true)
	// Project moves retain the session id and pin, while daemon move appends.
	a.st.Sessions = slices.DeleteFunc(a.st.Sessions, func(s proto.Session) bool { return s.ID == "s3" })
	a.st.Sessions = append(a.st.Sessions, proto.Session{ID: "s3", Project: "p2", Name: "s3"})
	a.st.Sessions = append(a.st.Sessions, proto.Session{ID: "new", Project: "p2", Name: "new"})
	groups := a.groups()
	var second []string
	for _, s := range groups[1].sessions {
		second = append(second, s.ID)
	}
	if !slices.Equal(second, []string{"s3", "other", "new"}) {
		t.Fatalf("moved pin and new session = %v", second)
	}
	if a.session("s3").Project != "p2" || a.prefs.SessionPins["new"] {
		t.Fatal("pinning changed project or pinned a new session")
	}
}
