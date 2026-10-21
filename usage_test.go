package main

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/resources"
	"kero/internal/usage"
	"kero/locales"
)

func TestUsageViews(t *testing.T) {
	a := testApp(t)
	a.usage.keys = map[string]*string{}
	a.usage.installed = []string{"claude", "codex", "opencode", "deepseek"}
	a.usage.reports = usage.Providers()
	cpu := float64(14)
	a.resources.sample = resources.Sample{CPU: &cpu, MemoryUsed: 18 << 30, MemoryTotal: 24 << 30}
	now := time.Now()
	a.usage.reports[0].Checked = now
	a.usage.reports[0].Windows = []usage.Window{{Label: "5 hours", Used: 27, Resets: now.Add(time.Hour)}, {Label: "Weekly", Used: 64, Resets: now.Add(72 * time.Hour)}}
	a.usage.reports[1].Checked = now.Add(-time.Hour)
	a.usage.reports[1].Windows = []usage.Window{{Label: "Weekly", Used: 91, Resets: now.Add(time.Hour)}}
	a.usage.reports[1].Error = "Could not reach provider"
	a.usage.reports[2].Error = "Sign in again"
	a.usage.reports[3].Error = "Not connected"
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(a.view, 1100, 640)
		tt.SetDark(dark)
		tt.Frame()
		// The sidebar is a ring for each harness, so the numbers live in
		// the card and the modal.
		for _, name := range []string{"Claude", "Codex", "OpenCode Go", "DeepSeek"} {
			if !tt.HasText(name) {
				t.Fatalf("missing harness %q: %q", name, tt.Texts())
			}
		}
		if tt.HasText("27% used") {
			t.Fatalf("the sidebar still spells out usage: %q", tt.Texts())
		}
		name := "usage-light"
		if dark {
			name = "usage-dark"
		}
		shot(t, tt, name+"-sidebar")
		a.openUsage()
		details := ui.NewTester(a.view, 1100, 800)
		details.SetDark(dark)
		details.Frame()
		if !details.HasText("Account usage") || !details.HasText("Weekly") || !details.HasText("Could not reach provider") || !details.HasText("27% used") {
			t.Fatalf("missing details: %q", details.Texts())
		}
		shot(t, details, name+"-details")
		a.closeUsage()
	}
	compact := ui.NewTester(a.view, 560, 360)
	compact.Frame()
	shot(t, compact, "usage-compact")
	a.openUsage()
	compact.Frame()
	close, ok := compact.Find("Close usage (Esc)")
	if !ok || close.X < 0 || close.Y < 0 || close.X+close.W > 560 || close.Y+close.H > 360 {
		t.Fatalf("modal close button outside compact window: %+v", close)
	}
	shot(t, compact, "usage-compact-modal")
	a.closeUsage()
}

// The harness bar folds into rows while its names fit, and shows the rings
// alone when three rows of them would leave no room for a name.
func TestUsageBarRows(t *testing.T) {
	room := []float32{72, 69, 93, 92}
	cases := []struct {
		avail float32
		rows  int
		named bool
	}{
		{424, 1, true},
		{252, 2, true},
		{184, 1, false},
	}
	for _, c := range cases {
		rows, named := usageBarRows(room, c.avail)
		seen := 0
		for _, line := range rows {
			seen += len(line)
		}
		if len(rows) != c.rows || named != c.named || seen != len(room) {
			t.Errorf("at %g wide: %v rows, named %v, %d harnesses; want %d, %v, %d",
				c.avail, len(rows), named, seen, c.rows, c.named, len(room))
		}
	}
}

// At the narrowest sidebar the rings stay whole while the names shorten or
// give way: an item still finds its harness, inside the sidebar.
func TestUsageRingsAtMinimumWidth(t *testing.T) {
	a := testApp(t)
	a.prefs.SidebarWidth = minSidebar
	a.usage.installed = []string{"claude", "codex", "opencode", "deepseek"}
	a.usage.reports = usage.Providers()
	now := time.Now()
	for i := range a.usage.reports {
		a.usage.reports[i].Checked = now
		a.usage.reports[i].Windows = []usage.Window{{Label: "Weekly", Used: 40, Resets: now.Add(2 * time.Hour)}}
	}
	tt := ui.NewTester(a.view, 560, 360)
	tt.Frame()
	for _, report := range a.usage.reports {
		r, ok := tt.Find(report.Name + " usage")
		if !ok {
			t.Fatalf("no ring for %s at %d wide: %q", report.Name, minSidebar, tt.Texts())
		}
		if r.W < 22 || r.X < 0 || r.X+r.W > minSidebar {
			t.Errorf("%s rings outside the %d sidebar: %+v", report.Name, minSidebar, r)
		}
	}
	shot(t, tt, "usage-min-width")
}

// Hovering a ring shows its card with the plan, each window and its reset,
// and moving off takes it away.
func TestUsageHoverCard(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testApp(t)
		a.usage.installed = []string{"claude", "codex", "opencode", "deepseek"}
		a.usage.reports = usage.Providers()
		now := time.Now()
		// As the API says it: the card capitalizes it.
		a.usage.reports[0].Plan = "max"
		a.usage.reports[0].Checked = now
		a.usage.reports[0].Windows = []usage.Window{
			{Label: "5 hours", Used: 27, Resets: now.Add(2 * time.Hour)},
			{Label: "Weekly", Used: 64, Resets: now.Add(72 * time.Hour)},
		}
		a.usage.reports[1].Checked = now
		a.usage.reports[1].Windows = []usage.Window{{Label: "Weekly", Used: 91, Resets: now.Add(-time.Minute)}}
		a.usage.reports[3].Balance = "USD 4.60"
		tt := ui.NewTester(a.view, 1100, 700)
		tt.SetDark(dark)
		tt.Frame()
		item, ok := tt.Find("Claude usage")
		if !ok {
			t.Fatalf("no Claude ring: %q", tt.Texts())
		}
		nameAt, _ := tt.Find("Claude")
		plain := inked(tt, nameAt)
		tt.Move(item.X+item.W/2, item.Y+item.H/2)
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
		// The hovered harness's name is heavier, in the text's color.
		if !dark && inked(tt, nameAt) <= plain {
			t.Errorf("the hovered name did not stand out: %d inked pixels, %d before", inked(tt, nameAt), plain)
		}
		for _, want := range []string{"Max", "27%", "64%", "5 hours", "Week", "resets ", usageReset(now.Add(2 * time.Hour))} {
			if !tt.HasText(want) {
				t.Fatalf("hover card lacks %q: %q", want, tt.Texts())
			}
		}
		name := "usage-hover-light"
		if dark {
			name = "usage-hover-dark"
		}
		shot(t, tt, name)
		if tt.HasText("Weekly") || tt.HasText("max") {
			t.Fatalf("the card spells the window or plan as the API does: %q", tt.Texts())
		}
		// The card stays while the pointer is on it.
		if r, ok := tt.Find("Week"); ok {
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			if !tt.HasText("Max") {
				t.Fatalf("the card closed as the pointer came to it: %q", tt.Texts())
			}
		} else {
			t.Fatal("no card to hover")
		}
		// A report that arrives while the card is open shows in it.
		a.usage.reports[0].Windows[0].Used = 55
		a.invalidateUsage()
		tt.Frame()
		if !tt.HasText("55%") {
			t.Fatalf("the open card kept stale numbers: %q", tt.Texts())
		}
		// A window due says so, rather than a time.
		if r, ok := tt.Find("Codex usage"); ok {
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(200 * time.Millisecond)
			tt.Frame()
			if !tt.HasText("resets now") || tt.HasText("Max") {
				t.Fatalf("the codex card: %q", tt.Texts())
			}
		} else {
			t.Fatal("no Codex ring")
		}
		tt.Move(900, item.Y)
		tt.Advance(500 * time.Millisecond)
		tt.Frame()
		if tt.HasText("Max") {
			t.Fatalf("the card stayed after the pointer left: %q", tt.Texts())
		}
	}
}

func TestUsageStates(t *testing.T) {
	r := usage.Report{Error: "Sign in again"}
	if got := usageSummary(r, true); got != "Checking…" {
		t.Fatal(got)
	}
	if got := usageSummary(r, false); got != "Connect" {
		t.Fatal(got)
	}
	r.Checked = time.Now().Add(-time.Hour)
	r.Windows = []usage.Window{{Used: 42}}
	if got := usageSummary(r, false); got != "42% used" {
		t.Fatalf("lost last observation: %q", got)
	}
	if got := resetLabel(time.Now().Add(-time.Second)); got != "Reset due · refresh to check" {
		t.Fatal(got)
	}
	now := time.Now()
	if got := usageReset(now.Add(-time.Minute)); got != "resets now" {
		t.Fatalf("due reset: %q", got)
	}
	if got := usageReset(now.Add(2 * time.Hour)); !strings.HasPrefix(got, "resets ") || strings.Contains(got, "resets resets") {
		t.Fatalf("reset later today: %q", got)
	}
	if got := usageReset(now.AddDate(0, 0, 3)); got != "resets "+now.AddDate(0, 0, 3).Format("Mon") {
		t.Fatalf("reset later this week: %q", got)
	}
	// A week's window reset the day after the last: its weekday would read
	// as today's, so the card gives the date.
	wed := time.Date(2026, 10, 21, 10, 0, 0, 0, time.Local)
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{wed.Add(2 * time.Hour), "resets 12:00 PM"},
		{wed.AddDate(0, 0, 2), "resets Fri"},
		{wed.AddDate(0, 0, 6), "resets Oct 27"},
		{wed.AddDate(0, 0, 7).Add(-time.Hour), "resets Oct 28"},
		{wed.AddDate(0, 0, 9), "resets Oct 30"},
	} {
		if got := usageResetAt(c.at, wed); got != c.want {
			t.Errorf("reset at %v: %q, want %q", c.at, got, c.want)
		}
	}
	for plan, want := range map[string]string{"max": "Max", "plus": "Plus", "pro": "Pro", "team_pro": "Team Pro", "élite": "Élite", "API credit": "API credit", "": ""} {
		if got := usagePlan(plan); got != want {
			t.Errorf("plan %q shows %q, want %q", plan, got, want)
		}
	}
}

// Each ring says what it can: the arc, a balance's sign, a dashed track
// for a harness not connected, and an amber dot when a refresh failed.
func TestUsageRingMarks(t *testing.T) {
	now := time.Now()
	used := usage.Report{Checked: now, Windows: []usage.Window{{Label: "Weekly", Used: 40}}}
	stale := used
	stale.Error = "Could not reach provider"
	cases := []struct {
		name    string
		r       usage.Report
		loading bool
		mark    usageMark
		dot     bool
	}{
		{"usage", used, false, markUsage, false},
		{"stale", stale, false, markUsage, true},
		{"balance", usage.Report{Checked: now, Balance: "USD 4.60"}, false, markBalance, false},
		{"not connected", usage.Report{Error: "Not connected"}, false, markUnconnected, false},
		{"signed out", usage.Report{Error: "Sign in again"}, false, markUnconnected, false},
		{"not connected, refreshing", usage.Report{Error: "Not connected"}, true, markUnconnected, false},
		{"failed first", usage.Report{Error: "Could not reach provider"}, false, markUsage, true},
		{"checking", usage.Report{}, true, markUsage, false},
	}
	for _, c := range cases {
		if mark, dot := usageMarkOf(c.r, c.loading); mark != c.mark || dot != c.dot {
			t.Errorf("%s: mark %d, dot %v; want %d, %v", c.name, mark, dot, c.mark, c.dot)
		}
	}
	// The marks draw differently from one another, and from 0%.
	pal := lightPalette
	ink := func(r usage.Report) *image.RGBA {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Row(c).Padding(4).Children(func() { usageRing(c, &pal, r, false, false) })
		}, 30, 30)
		tt.Frame()
		return tt.Image()
	}
	empty := usage.Report{Checked: now, Windows: []usage.Window{{Label: "Weekly", Used: 0}}}
	shots := map[string]*image.RGBA{"0%": ink(empty)}
	for _, c := range cases {
		switch c.name {
		case "stale", "balance", "not connected", "failed first":
			shots[c.name] = ink(c.r)
		}
	}
	for a, x := range shots {
		for b, y := range shots {
			if a < b && bytes.Equal(x.Pix, y.Pix) {
				t.Errorf("the %s and %s rings look the same", a, b)
			}
		}
	}
}

// A card over the last known usage says a refresh failed.
func TestUsageHoverStale(t *testing.T) {
	a := testApp(t)
	a.usage.installed = []string{"claude"}
	a.usage.reports = usage.Providers()
	now := time.Now()
	a.usage.reports[0].Checked = now.Add(-time.Hour)
	a.usage.reports[0].Windows = []usage.Window{{Label: "5 hours", Used: 27, Resets: now.Add(time.Hour)}}
	a.usage.reports[0].Error = "Could not reach provider"
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	item, ok := tt.Find("Claude usage")
	if !ok {
		t.Fatalf("no Claude ring: %q", tt.Texts())
	}
	tt.Move(item.X+item.W/2, item.Y+item.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if !tt.HasText(locales.UsageStale()) || !tt.HasText("27%") {
		t.Fatalf("the stale card: %q", tt.Texts())
	}
	shot(t, tt, "usage-hover-stale")
}

func TestUsageKeyEditing(t *testing.T) {
	a := testApp(t)
	a.usage.keys = map[string]*string{}
	r := usage.Providers()[2]
	tt := ui.NewTester(func(c *ui.Context) { a.usageCard(c, a.paletteFor(c), r) }, 520, 360)
	tt.Frame()
	if err := tt.Click("OpenCode Go API key"); err != nil {
		t.Fatal(err)
	}
	tt.Type("test-key")
	tt.Frame()
	tt.Frame()
	if a.usage.keys["opencode"] == nil || *a.usage.keys["opencode"] != "test-key" {
		t.Fatal("API key edit was lost")
	}
}

func TestUsageOpensInsideCurrentWindow(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	if err := tt.Click("Claude usage"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Account usage") || !tt.HasText("Refresh") {
		t.Fatalf("usage modal missing: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("Refresh") {
		t.Fatal("Escape did not dismiss usage modal")
	}
	// Reopening restores a usable modal, then Cmd+W closes it rather than a pane.
	a.run("usage.open", nil)
	tt.Frame()
	a.run("session.rename", nil)
	if a.renaming != "" {
		t.Fatal("modal refresh shortcut renamed the underlying session")
	}
	a.run("pane.close", nil)
	tt.Frame()
	if a.usage.open || len(a.st.Sessions) != 1 {
		t.Fatal("closing usage affected the session")
	}
}
