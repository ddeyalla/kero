package main

import (
	"fmt"
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
	r.Windows = []usage.Window{{Label: "5 hours", Used: 42}, {Label: "Weekly", Used: 61}}
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

// Missing data is a neutral track, never invented usage; stale data keeps
// exactly the last observed arcs, with its error explained only in the card.

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

func TestUsageHoverCard(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testApp(t)
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
			t.Fatalf("no Claude row: %q", tt.Texts())
		}
		tt.Move(item.X+item.W/2, item.Y+item.H/2)
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
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
		if tt.HasText("max") {
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
			t.Fatal("no Codex row")
		}
		tt.Move(900, item.Y)
		tt.Advance(500 * time.Millisecond)
		tt.Frame()
		if tt.HasText("Max") {
			t.Fatalf("the card stayed after the pointer left: %q", tt.Texts())
		}
	}
}

func TestUsageHoverStale(t *testing.T) {
	a := testApp(t)
	a.usage.reports = usage.Providers()
	now := time.Now()
	a.usage.reports[0].Checked = now.Add(-time.Hour)
	a.usage.reports[0].Windows = []usage.Window{{Label: "5 hours", Used: 27, Resets: now.Add(time.Hour)}}
	a.usage.reports[0].Error = "Could not reach provider"
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	item, ok := tt.Find("Claude usage")
	if !ok {
		t.Fatalf("no Claude row: %q", tt.Texts())
	}
	tt.Move(item.X+item.W/2, item.Y+item.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if !tt.HasText(locales.UsageStale()) || !tt.HasText("27%") {
		t.Fatalf("the stale card: %q", tt.Texts())
	}
	shot(t, tt, "usage-hover-stale")
}

// The hover card retains every allowance window, not just the list's primary one.
func TestUsageThreeWindowHover(t *testing.T) {
	a := testApp(t)
	r := usage.Providers()[2]
	r.Checked = time.Now()
	for i, label := range []string{"5 hours", "Weekly", "Monthly"} {
		r.Windows = append(r.Windows, usage.Window{Label: label, Used: float64(30 + i*20), Resets: time.Now().Add(time.Duration(i+1) * time.Hour)})
	}
	a.usage.reports = []usage.Report{r}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	item, ok := tt.Find(r.Name + " usage")
	if !ok {
		t.Fatal("no OpenCode item")
	}
	tt.Move(item.X+item.W/2, item.Y+item.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	for _, w := range r.Windows {
		for _, want := range []string{usageWindow(w.Label), fmt.Sprintf("%.0f%%", w.Used), "· " + usageReset(w.Resets)} {
			if _, ok := tt.Find(want); !ok {
				t.Errorf("hover card missing %q: %q", want, tt.Texts())
			}
		}
	}
}

// usageListFixture matches the previous list's values while retaining richer
// window and balance data for the hover card. It never reads real credentials.
func usageListFixture(t *testing.T) *App {
	a := testApp(t)
	a.usage.reports = usage.Providers()
	now := time.Now()
	for i := range a.usage.reports {
		r := &a.usage.reports[i]
		r.Checked = now
		r.Windows = []usage.Window{{Label: "5 hours", Used: []float64{2, 90, 0, 0}[i], Resets: now.Add(time.Hour)}, {Label: "Weekly", Used: 64, Resets: now.AddDate(0, 0, 2)}}
	}
	a.usage.reports[0].Plan = "max"
	a.usage.reports[3].Windows = nil
	a.usage.reports[3].Balance = "CNY 80.00 · USD 10.77"
	return a
}

func TestUsageListLayouts(t *testing.T) {
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for _, width := range []float32{minSidebar, defaultSidebar} {
			t.Run(fmt.Sprintf("%s-%g", appearance, width), func(t *testing.T) {
				a := usageListFixture(t)
				a.prefs.SidebarWidth = width
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Background(a.paletteFor(c).sidebar).Children(func() { a.usageSidebar(c, a.paletteFor(c)) })
				}, int(width), 150)
				tt.SetDark(dark)
				tt.Frame()
				for _, text := range []string{"Usage", a.keys("usage.open"), "Claude", "Codex", "OpenCode Go", "DeepSeek", "2% used", "90% used", "0% used", "USD 10.77"} {
					r, ok := tt.Find(text)
					if !ok {
						t.Fatalf("missing list text %q: %q", text, tt.Texts())
					}
					if r.X < 12 || r.X+r.W > width-12+0.5 {
						t.Errorf("list text %q clipped: %+v", text, r)
					}
				}
				if tt.HasText("64%") || tt.HasText("CNY") {
					t.Fatalf("list should show only primary usage and USD: %q", tt.Texts())
				}
				previousY := float32(-1)
				for _, report := range a.usage.reports {
					row, ok := tt.Find(report.Name + " usage")
					if !ok || row.H != 25 || row.Y <= previousY || row.X < 0 || row.X+row.W > width {
						t.Errorf("invalid list row: %+v, found %v", row, ok)
					}
					previousY = row.Y
					if _, ok := tt.Find(report.Name + " meter"); ok {
						t.Fatal("list still contains a circular meter")
					}
				}
				shot(t, tt, fmt.Sprintf("usage-list-%s-%g", appearance, width))
				if err := tt.Click("Claude usage"); err != nil {
					t.Fatal(err)
				}
				if !a.usage.open {
					t.Fatal("list row did not open account details")
				}
			})
		}
	}
}

func TestUsageListColorsAndSummaries(t *testing.T) {
	now := time.Now()
	for _, pal := range []*palette{&lightPalette, &darkPalette} {
		for _, c := range []struct {
			used    float64
			problem string
			want    ui.Color
		}{
			{89.9, "", pal.textMuted}, {90, "", pal.attention}, {100, "", pal.attention}, {95, "Could not reach provider", pal.textFaint},
		} {
			r := usage.Report{Checked: now, Error: c.problem, Windows: []usage.Window{{Used: c.used}}}
			if got := usageColor(r, pal); got != c.want {
				t.Errorf("usage %g error %q: color %v, want %v", c.used, c.problem, got, c.want)
			}
		}
	}
	r := usage.Report{Checked: now, Balance: "CNY 80.00 · USD 10.77"}
	if got := usageFooterSummary(r, false); got != "USD 10.77" {
		t.Fatal(got)
	}
	if got := usageSummary(r, false); got != r.Balance {
		t.Fatal("full summary lost balance currencies")
	}
	r = usage.Report{Error: "Sign in again"}
	if got := usageFooterSummary(r, false); got != "Connect" {
		t.Fatalf("missing data invented usage: %s", got)
	}
}

// The compact row shows the primary allowance, but a later exhausted window
// must still warn. Failed refreshes keep the last observation faint.
func TestUsageListWarnsForEveryAllowance(t *testing.T) {
	for _, pal := range []*palette{&lightPalette, &darkPalette} {
		for _, tc := range []struct {
			name    string
			windows []usage.Window
			problem string
			want    ui.Color
		}{
			{"weekly warning", []usage.Window{{Used: 12}, {Used: 90}}, "", pal.attention},
			{"monthly exhausted", []usage.Window{{Used: 12}, {Used: 35}, {Used: 100}}, "", pal.attention},
			{"below threshold", []usage.Window{{Used: 12}, {Used: 89.9}}, "", pal.textMuted},
			{"stale warning", []usage.Window{{Used: 12}, {Used: 100}}, "Refresh failed", pal.textFaint},
			{"no allowance", nil, "", pal.textMuted},
		} {
			if got := usageColor(usage.Report{Windows: tc.windows, Error: tc.problem}, pal); got != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		}
	}
}

func TestUsageListHoverLayouts(t *testing.T) {
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for _, width := range []float32{minSidebar, defaultSidebar} {
			t.Run(fmt.Sprintf("%s-%g", appearance, width), func(t *testing.T) {
				a := usageListFixture(t)
				a.prefs.SidebarWidth = width
				a.usage.reports[0].Windows[0].Used = 83
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Background(a.paletteFor(c).sidebar).Children(func() { ui.Spacer(c); a.usageSidebar(c, a.paletteFor(c)) })
				}, int(max(width, 260)), 300)
				tt.SetDark(dark)
				tt.Frame()
				hover := func(name string) {
					t.Helper()
					// Leave an open card before crossing the rows it overlays.
					tt.Move(320, 299)
					tt.Advance(500 * time.Millisecond)
					tt.Frame()
					row, ok := tt.Find(name + " usage")
					if !ok {
						t.Fatalf("missing %s row", name)
					}
					tt.Move(row.X+row.W/2, row.Y+row.H/2)
					tt.Advance(200 * time.Millisecond)
					tt.Frame()
				}
				hover("Claude")
				for _, text := range []string{"Max", "83%", "64%", "Week", "· " + usageReset(a.usage.reports[0].Windows[0].Resets)} {
					if !tt.HasText(text) {
						t.Fatalf("missing hover text %q: %q", text, tt.Texts())
					}
				}
				first, _ := tt.Find("83%")
				second, _ := tt.Find("64%")
				if first.X != second.X {
					t.Errorf("percent columns differ: %+v, %+v", first, second)
				}
				shot(t, tt, fmt.Sprintf("usage-list-hover-%s-%g", appearance, width))
				hover("DeepSeek")
				if !tt.HasText(locales.UsageBalanceRemaining(a.usage.reports[3].Balance)) {
					t.Fatal("hover lost full balance")
				}
				shot(t, tt, fmt.Sprintf("usage-list-balance-%s-%g", appearance, width))
				a.usage.reports[2].Checked = time.Time{}
				a.usage.reports[2].Windows = nil
				a.usage.reports[2].Error = "Sign in again"
				hover("OpenCode Go")
				if !tt.HasText("Sign in again") || tt.HasText("64%") {
					t.Fatalf("disconnected hover: %q", tt.Texts())
				}
				shot(t, tt, fmt.Sprintf("usage-list-disconnected-%s-%g", appearance, width))
				a.usage.reports[0].Error = "Could not reach provider"
				hover("Claude")
				if !tt.HasText(locales.UsageStale()) || !tt.HasText("Could not reach provider") || !tt.HasText("83%") {
					t.Fatalf("stale hover: %q", tt.Texts())
				}
				shot(t, tt, fmt.Sprintf("usage-list-stale-%s-%g", appearance, width))
				// A full allowance needs three digits plus the percent sign,
				// on one baseline even in the narrowest sidebar.
				a.usage.reports[0].Error = ""
				a.usage.reports[0].Windows[0].Used = 100
				tt.Frame()
				full, ok := tt.Find("100%")
				if !ok || full.H > 20 {
					t.Fatalf("100%% hover value wrapped or missing: %+v, found %v", full, ok)
				}
				week, _ := tt.Find("64%")
				if full.X != week.X || full.W != week.W || abs(full.H-week.H) > 0.5 {
					t.Errorf("100%% changed the percentage column: %+v, week %+v", full, week)
				}
				shot(t, tt, fmt.Sprintf("usage-list-full-%s-%g", appearance, width))
			})
		}
	}
}
