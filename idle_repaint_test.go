package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/internal/resources"
)

// A reading that moves by less than the labels and bars show is the same
// to the app, so the sampler does not rebuild the window for it; one that
// changes a label, a bar or the error is not.
func TestResourcesShownOnlyChangeWithWhatShows(t *testing.T) {
	sample := func(cpu float64, used uint64) resources.Sample {
		return resources.Sample{CPU: &cpu, MemoryUsed: used, MemoryTotal: 32 << 30}
	}
	base := showResources(sample(12.2, 18<<30), "")
	if same := showResources(sample(11.8, 18<<30+1<<20), ""); same != base {
		t.Errorf("a reading that shows the same differs: %+v, %+v", base, same)
	}
	for name, other := range map[string]resourceShown{
		"CPU label":   showResources(sample(13.4, 18<<30), ""),
		"RAM label":   showResources(sample(12.2, 19<<30), ""),
		"no CPU":      showResources(resources.Sample{MemoryUsed: 18 << 30, MemoryTotal: 32 << 30}, ""),
		"a read fail": showResources(sample(12.2, 18<<30), "no stats"),
	} {
		if other == base {
			t.Errorf("a reading with another %s shows the same", name)
		}
	}
	failed := showResources(resources.Sample{}, "no stats")
	if again := showResources(resources.Sample{}, "no stats"); again != failed {
		t.Error("the same failure differs")
	}
	if failed == base {
		t.Error("recovering from a failure shows the same")
	}
}

// idleBuilds counts how often the window of a builds in two minutes and a
// second while nothing happens, with the sidebar shown or hidden.
func idleBuilds(t *testing.T, a *App, hidden bool) int {
	t.Helper()
	a.prefs.SidebarHidden = hidden
	n := 0
	tt := ui.NewTester(func(c *ui.Context) { n++; a.view(c) }, 1100, 640)
	tt.Frame()
	tt.Advance(time.Second) // what moves as the window opens
	n = 0
	tt.Advance(2*time.Minute + time.Second)
	return n
}

// The times ago in the sidebar move on once a minute while it shows, and
// an idle window with it hidden does not build for them; a view that
// shows times ago with the sidebar hidden, the attention pane, still
// moves them on.
func TestIdleAgoTick(t *testing.T) {
	a := testApp(t)
	// Panes of the attention view, with nothing to list yet: a terminal
	// blinks its cursor, which builds the window of its own.
	for i := range a.st.Sessions[0].Panes {
		p := &a.st.Sessions[0].Panes[i]
		p.View, p.Shell = proto.PaneAttention, ""
		p.Since = time.Now().Add(-90 * time.Second).UnixMilli()
	}
	shown := idleBuilds(t, a, false)
	hidden := idleBuilds(t, a, true)
	if shown-hidden < 2 || shown-hidden > 3 {
		t.Errorf("the sidebar built the window %d times in two minutes more than with it hidden, want about 2", shown-hidden)
	}
	if hidden != 0 {
		t.Errorf("the idle window built %d times in two minutes with the sidebar hidden", hidden)
	}

	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: "s2", Project: "p1", Name: "waits",
		Panes:     []proto.Pane{{ID: "c", Status: proto.Waiting, Progress: -1}},
		Attention: &proto.Attention{State: proto.AttentionWaitingUser, Pane: "c", Since: time.Now().UnixMilli()},
	})
	if n := idleBuilds(t, a, true); n < 2 {
		t.Errorf("the attention pane built %d times in two minutes with the sidebar hidden, want its times ago to move on", n)
	}
}
