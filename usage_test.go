package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/resources"
	"kero/internal/usage"
)

func TestUsageViews(t *testing.T) {
	a := testApp(t)
	a.usage.keys = map[string]*string{}
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
		if !tt.HasText("27% used") || !tt.HasText("91% used") || !tt.HasText("Connect") {
			t.Fatalf("missing account state: %q", tt.Texts())
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
		if !details.HasText("Account usage") || !details.HasText("Weekly") || !details.HasText("Could not reach provider") {
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
	if err := tt.Click("Account usage"); err != nil {
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
