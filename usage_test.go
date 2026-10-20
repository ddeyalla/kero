package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/usage"
)

func TestUsageViews(t *testing.T) {
	a := testApp(t)
	a.usage.keys = map[string]*string{}
	a.usage.reports = usage.Providers()
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
		details := ui.NewTester(a.usageView, 520, 680)
		details.SetDark(dark)
		details.Frame()
		if !details.HasText("Account usage") || !details.HasText("Weekly") || !details.HasText("Could not reach provider") {
			t.Fatalf("missing details: %q", details.Texts())
		}
		shot(t, details, name+"-details")
	}
	compact := ui.NewTester(a.view, 560, 360)
	compact.Frame()
	shot(t, compact, "usage-compact")
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
	tt := ui.NewTester(func(c *ui.Context) { a.usageCard(c, paletteFor(c), r) }, 520, 360)
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
