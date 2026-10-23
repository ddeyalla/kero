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

func projectSidebarApp(t *testing.T) *App {
	a := sidebarTestApp(t)
	a.st.Projects = append(a.st.Projects, proto.Project{ID: "p3", Name: "third"})
	return a
}

func TestProjectMoveDestination(t *testing.T) {
	a := projectSidebarApp(t)
	for _, tc := range []struct {
		id, target string
		after      bool
		index      int
	}{
		{"p1", "p2", true, 1},
		{"p1", "p3", true, 2},
		{"p3", "p1", false, 0},
		{"p3", "p1", true, 1},
		{"p2", "p3", true, 2},
	} {
		got, ok := a.projectMove(tc.id, tc.target, tc.after)
		if !ok || got != (proto.MoveProjectParams{ID: tc.id, Index: tc.index}) {
			t.Fatalf("move(%s,%s,%t) = %+v,%t", tc.id, tc.target, tc.after, got, ok)
		}
	}
	for _, pair := range [][2]string{{"", "p1"}, {"p1", ""}, {"p1", "p1"}, {"p1", "p2"}, {"missing", "p1"}, {"p1", "missing"}} {
		if _, ok := a.projectMove(pair[0], pair[1], false); ok {
			t.Fatalf("accepted invalid move %v", pair)
		}
	}
	if _, ok := a.projectMove("p2", "p1", true); ok {
		t.Fatal("accepted unchanged order")
	}
	for _, tc := range []struct {
		id     string
		offset int
		want   string
	}{
		{"p1", -1, ""}, {"p1", 1, "p2"}, {"p2", -1, "p1"}, {"p2", 1, "p3"}, {"p3", 1, ""}, {"", 1, ""}, {"missing", 1, ""},
	} {
		if got := a.projectNeighbor(tc.id, tc.offset); got != tc.want {
			t.Fatalf("neighbor(%s,%d) = %q, want %q", tc.id, tc.offset, got, tc.want)
		}
	}
}

// This listener records only project moves; it never starts a real daemon.
func projectSidebarRPC(t *testing.T, a *App) <-chan proto.MoveProjectParams {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "project-rpc-")
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
	moves := make(chan proto.MoveProjectParams, 10)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec, enc := json.NewDecoder(conn), json.NewEncoder(conn)
		var hello proto.Hello
		if dec.Decode(&hello) != nil || enc.Encode(proto.Welcome{Version: proto.Version}) != nil {
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
			if req.Method == "project.move" {
				var p proto.MoveProjectParams
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

func wantProjectSidebarMove(t *testing.T, moves <-chan proto.MoveProjectParams, want proto.MoveProjectParams) {
	t.Helper()
	select {
	case got := <-moves:
		if got != want {
			t.Fatalf("project.move = %+v, want %+v", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("project move did not reach daemon")
	}
}

func TestProjectOrderActions(t *testing.T) {
	a := projectSidebarApp(t)
	moves := projectSidebarRPC(t, a)
	a.projectOrderAction("project.moveDown", nil)
	wantProjectSidebarMove(t, moves, proto.MoveProjectParams{ID: "p1", Index: 1})
	a.projectOrderAction("project.moveUp", map[string]string{"project": "p3"})
	wantProjectSidebarMove(t, moves, proto.MoveProjectParams{ID: "p3", Index: 1})
	if a.st.Projects[0].ID != "p1" || a.st.Projects[2].ID != "p3" {
		t.Fatal("order changed before daemon confirmation")
	}
}

func TestProjectOrderMenu(t *testing.T) {
	a := projectSidebarApp(t)
	moves := projectSidebarRPC(t, a)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if err := tt.RightClick("second"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), locales.ProjectMoveUp()) || !slices.Contains(tt.Menu(), locales.ProjectMoveDown()) {
		t.Fatal("project menu does not expose ordering")
	}
	if err := tt.ChooseMenuItem(locales.ProjectMoveUp()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	wantProjectSidebarMove(t, moves, proto.MoveProjectParams{ID: "p2", Index: 0})
}

func TestProjectOrderUnavailable(t *testing.T) {
	a := projectSidebarApp(t)
	a.reorderProject("p1", "p3", true)
	if a.pendingToast != locales.SessionOrderUnavailable() {
		t.Fatalf("missing disconnected explanation: %q", a.pendingToast)
	}
	if a.st.Projects[0].ID != "p1" {
		t.Fatal("disconnected move changed project order")
	}
}

func TestProjectOrderMissing(t *testing.T) {
	a := projectSidebarApp(t)
	a.projectOrderAction("project.moveUp", map[string]string{"project": "missing"})
	if a.pendingToast != locales.ProjectOrderMissing() {
		t.Fatalf("missing project explanation: %q", a.pendingToast)
	}
	a.pendingToast = ""
	a.selected = "missing"
	a.projectOrderAction("project.moveDown", nil)
	if a.pendingToast != locales.ProjectOrderMissing() {
		t.Fatalf("missing selected project explanation: %q", a.pendingToast)
	}
}
