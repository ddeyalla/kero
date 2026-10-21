package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/resources"
	"kero/internal/usage"
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
		a.usage.reports[0].Plan = "Max"
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
		tt.Move(item.X+item.W/2, item.Y+item.H/2)
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
		for _, want := range []string{"Max", "27%", "64%", "resets ", usageReset(now.Add(2 * time.Hour))} {
			if !tt.HasText(want) {
				t.Fatalf("hover card lacks %q: %q", want, tt.Texts())
			}
		}
		name := "usage-hover-light"
		if dark {
			name = "usage-hover-dark"
		}
		shot(t, tt, name)
		// The card stays while the pointer is on it.
		if r, ok := tt.Find("Weekly"); ok {
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
