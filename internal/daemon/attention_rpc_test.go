package daemon

import (
	"encoding/json"
	"testing"

	"kero/internal/proto"
)

// Navigation sends focus even when a stale client thinks the pane already
// has it. The daemon follows its actual zoom without toggling that zoom.
func TestAttentionFocusRPCIsIdempotent(t *testing.T) {
	d := linearTestDaemon(t)
	s := d.sessions[0]
	p := &pane{info: proto.Pane{ID: "other"}}
	s.panes = append(s.panes, p)
	for _, zoom := range []string{"", "p", "other"} {
		s.focus, s.zoomed = "p", zoom
		params, _ := json.Marshal(proto.FocusParams{Session: "s", Pane: "p"})
		for range 2 {
			if _, err := d.call(nil, "focus", params); err != nil {
				t.Fatal(err)
			}
		}
		want := ""
		if zoom != "" {
			want = "p"
		}
		if s.focus != "p" || s.zoomed != want {
			t.Fatalf("from zoom %q: focus %q zoom %q", zoom, s.focus, s.zoomed)
		}
	}
	// A row reveals the full layout with an explicit clear, irrespective of
	// whatever zoom a client last received.
	s.zoomed = "other"
	params, _ := json.Marshal(proto.FocusParams{Session: "s"})
	for range 2 {
		if _, err := d.call(nil, "zoom", params); err != nil {
			t.Fatal(err)
		}
	}
	if s.zoomed != "" {
		t.Fatalf("explicit clear left zoom %q", s.zoomed)
	}
}

// Attention state uses the existing saved target format unchanged.
func TestAttentionTargetPersistsThroughDaemonRestore(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	target := `{"filter":"waiting-user","query":"approve","scroll":104}`
	s, err := d.create(proto.CreateParams{View: proto.PaneAttention, Target: target, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	d.saveState()
	restored := linearTestDaemon(t)
	restored.dir = d.dir
	restored.sessions, restored.panes = nil, map[string]*pane{}
	restoreAll(t, restored)
	if len(restored.sessions) != 1 {
		t.Fatalf("restored %d sessions", len(restored.sessions))
	}
	p := restored.panes[s.Focus]
	if p == nil || p.info.Target != target || p.info.View != proto.PaneAttention {
		t.Fatalf("restored pane %+v", p)
	}
}

func TestAttentionActionRejectsClosedSession(t *testing.T) {
	d := linearTestDaemon(t)
	sub := &controlConn{out: make(chan proto.Message, 2)}
	d.subs = map[*controlConn]struct{}{sub: {}}
	args, _ := json.Marshal(proto.DoParams{Action: "session.attention", Args: map[string]string{"session": "closed-session"}})
	if _, err := d.call(nil, "do", args); err == nil {
		t.Fatal("stale detail's closed session action accepted")
	}
	select {
	case m := <-sub.out:
		t.Fatalf("invalid action broadcast: %+v", m)
	default:
	}
	// The same action still accepts a current session and canonicalizes its name.
	args, _ = json.Marshal(proto.DoParams{Action: "session.attention", Args: map[string]string{"session": "working"}})
	if _, err := d.call(nil, "do", args); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-sub.out:
		if m.Args["session"] != "s" {
			t.Fatalf("uncanonical target %q", m.Args["session"])
		}
	default:
		t.Fatal("valid action not broadcast")
	}
}
