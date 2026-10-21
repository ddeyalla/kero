package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"kero/internal/usage"
	"kero/locales"
)

type usageState struct {
	reports []usage.Report
	// installed are the providers this Mac has set up, the ones the sidebar
	// shows: usage.Installed's, kept from the last refresh.
	installed []string
	loading   bool
	ctx       context.Context
	cancel    context.CancelFunc
	open      bool
	scroll    ui.ScrollState
	keys      map[string]*string
	notice    string
	saving    bool
	// hover is the harness whose card shows, and cardOver tells the pointer
	// is on that card, which keeps it open as the pointer leaves the ring.
	hover    string
	cardOver bool
}

func (a *App) startUsage() {
	a.usage.ctx, a.usage.cancel = context.WithCancel(context.Background())
	// What is installed, as the reports, comes with the first refresh, off
	// the main thread: it looks through the disk.
	a.refreshUsage()
	ctx := a.usage.ctx
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.update(a.refreshUsage)
			}
		}
	}()
}

func (a *App) refreshUsage() {
	if a.usage.loading || a.usage.ctx == nil {
		return
	}
	a.usage.loading = true
	previous, ctx := a.usage.reports, a.usage.ctx
	a.invalidateUsage()
	go func() {
		installed := usage.Installed()
		reports := usage.Refresh(ctx, previous)
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			a.usage.reports, a.usage.installed, a.usage.loading = reports, installed, false
			a.invalidateUsage()
		})
	}()
}

func (a *App) invalidateUsage() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *App) usageReports() []usage.Report {
	if len(a.usage.reports) == 0 {
		return usage.Providers()
	}
	return a.usage.reports
}

// usageInstalled are the reports the sidebar shows: the harnesses this Mac
// has set up, in the providers' order. Until the first refresh finds them,
// every provider shows, as a new user sees what to connect.
func (a *App) usageInstalled() []usage.Report {
	reports := a.usageReports()
	if len(a.usage.installed) == 0 {
		return reports
	}
	have := make(map[string]bool, len(a.usage.installed))
	for _, id := range a.usage.installed {
		have[id] = true
	}
	out := make([]usage.Report, 0, len(reports))
	for _, r := range reports {
		if have[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

func usageSummary(r usage.Report, loading bool) string {
	if r.Checked.IsZero() {
		if loading {
			return "Checking…"
		}
		if r.Error != "" {
			if r.Error == "Not connected" || r.Error == "Sign in again" {
				return "Connect"
			}
			return "Unavailable"
		}
		return "Connect"
	}
	if r.Balance != "" {
		return r.Balance
	}
	if len(r.Windows) > 0 {
		return fmt.Sprintf("%.0f%% used", r.Windows[0].Used)
	}
	return "Unavailable"
}

// The harness bar's measures: a 22pt ring, its gap to the name, and the
// padding around both.
const (
	usageRingSize = 22
	usageNameGap  = 5
	usageItemPad  = 8
)

// usageSidebar is the harness bar above the machine's meters: a ring for
// each harness this Mac has set up, its name beside it where it fits, and,
// on hover, a card with the allowances the modal shows in full.
func (a *App) usageSidebar(c *ui.Context, pal *palette) {
	reports := a.usageInstalled()
	// Each item wants room for its whole name: the ring, its gap, the name
	// and the padding.
	room := make([]float32, len(reports))
	for i, r := range reports {
		// At the weight of the hovered name, so that the bar does not move
		// as the pointer goes over it.
		w, _ := c.MeasureText(0, ui.Span{Text: r.Name, Size: 11.5, Weight: 600})
		room[i] = usageRingSize + usageNameGap + w + usageItemPad + 1.5
	}
	avail := a.prefs.SidebarWidth - 16
	rows, named := usageBarRows(room, avail)
	var anchor ui.Element
	var shown usage.Report
	var card bool
	ui.Column(c.Key("account-usage")).Width(a.prefs.SidebarWidth).Shrink(0).Padding(3, 8, 4, 8).Gap(1).Children(func() {
		for _, line := range rows {
			ui.Row(c).Fill().Justify(ui.SpaceBetween).Gap(2).Children(func() {
				for _, i := range line {
					w := room[i]
					if !named {
						w = usageRingSize + usageItemPad
					}
					w = min(w, avail)
					item := a.usageHarness(c, pal, reports[i], w, named)
					if a.usage.hover == reports[i].ID {
						anchor, shown, card = item, reports[i], true
					}
				}
			})
		}
	})
	if card {
		a.usageHover(c, pal, shown, anchor)
	}
}

// usageBarRows arranges the harnesses in the sidebar's width, by their
// widths with their names: one row while they all fit, else as many rows
// as the names take, two at most. A bar that would need three rows has no
// room for names, and shows the rings alone, which the hover card names.
func usageBarRows(room []float32, avail float32) ([][]int, bool) {
	const gap = 2
	all := make([]int, len(room))
	var total float32
	for i, w := range room {
		all[i] = i
		total += w
		if i > 0 {
			total += gap
		}
	}
	if len(room) == 0 || total <= avail {
		return [][]int{all}, len(room) > 0
	}
	var rows [][]int
	var line []int
	var used float32
	for i, w := range room {
		add := w
		if len(line) > 0 {
			add += gap
		}
		if len(line) > 0 && used+add > avail {
			rows = append(rows, line)
			line, used, add = nil, 0, w
		}
		line = append(line, i)
		used += add
	}
	if len(line) > 0 {
		rows = append(rows, line)
	}
	// Balance the last row with the one before it, so that a ring does
	// not sit alone under a full row.
	if n := len(rows); n > 1 && len(rows[n-1]) == 1 && len(rows[n-2]) >= 3 {
		prev := rows[n-2]
		moved := prev[len(prev)-1]
		rows[n-2] = prev[:len(prev)-1]
		rows[n-1] = append([]int{moved}, rows[n-1]...)
	}
	if len(rows) > 2 {
		return [][]int{all}, false
	}
	return rows, true
}

// usageHarness is one ring and, where there is room for it, its name, in
// width DIPs, a click opening the modal. Its card stays while the pointer
// goes from the ring to the card.
func (a *App) usageHarness(c *ui.Context, pal *palette, r usage.Report, width float32, named bool) ui.Element {
	key := "usage-" + r.ID
	summary := usageSummary(r, a.usage.loading)
	item := ui.ButtonBase(c.Key(key)).Width(width).MinWidth(0).Height(28).Padding(0, usageItemPad/2).Radius(5).
		Label(r.Name + " usage").Description(a.tip(summary+" · Click for details", "usage.open"))
	if item.Hovered() {
		item.Background(pal.rowHover)
	}
	// The harness whose card shows stands out: its name in the text's
	// color and heavier, its ring's track darker.
	hot := item.Hovered() || a.usage.hover == r.ID
	item.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(usageNameGap).Children(func() {
			usageRing(c, pal, r, a.usage.loading, hot)
			// A name with no room would show only an ellipsis: let the ring
			// speak there, and the card on hover.
			if named && width-usageRingSize-usageNameGap-usageItemPad >= 16 {
				name := ui.Text(c, r.Name).FontSize(11.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				if hot {
					name.TextColor(pal.text).FontWeight(600)
				}
			}
		})
	})
	if item.Clicked() {
		a.openUsage()
	}
	over := item.Hovered() || a.usage.hover == r.ID && a.usage.cardOver
	stays := a.hoverIntent(c, key, over)
	switch {
	case item.Hovered():
		a.usage.hover = r.ID
	case a.usage.hover == r.ID && !stays:
		a.usage.hover, a.usage.cardOver = "", false
	}
	return item
}

// usageMark is what a harness's ring shows.
type usageMark uint8

const (
	// markUsage is the first allowance as the arc, the track alone at 0%
	// or while the first read is under way.
	markUsage usageMark = iota
	// markBalance is a balance, with no allowance to draw: a currency's
	// sign in the track.
	markBalance
	// markUnconnected is a harness with no sign-in to read: a dashed track.
	markUnconnected
)

// usageMarkOf is the mark r's ring shows, and whether an amber dot marks
// it: a refresh failed, so the arc is the last known usage, or there is
// none yet for another reason than a sign-in. A harness not connected
// stays so through a refresh; loading only matters to one never read.
func usageMarkOf(r usage.Report, loading bool) (usageMark, bool) {
	stale := r.Error != ""
	switch {
	case r.Balance != "" && len(r.Windows) == 0:
		return markBalance, stale
	case r.Checked.IsZero() && (r.Error == "Not connected" || r.Error == "Sign in again"):
		return markUnconnected, false
	case r.Checked.IsZero() && r.Error == "" && !loading:
		return markUnconnected, false
	}
	return markUsage, stale
}

// usageRing is a harness's first allowance as a ring: the track faint, the
// arc in the text's color, and amber once any window reaches 90%; a
// balance's currency sign, or a dashed track for a harness not connected;
// and an amber dot where a refresh failed. hot darkens the track, for the
// harness whose card shows.
func usageRing(c *ui.Context, pal *palette, r usage.Report, loading, hot bool) {
	mark, dot := usageMarkOf(r, loading)
	used := float32(0)
	alert := false
	if len(r.Windows) > 0 && !r.Checked.IsZero() {
		used = float32(max(0, min(100, r.Windows[0].Used)))
	}
	for _, w := range r.Windows {
		if w.Used >= 90 {
			alert = true
		}
	}
	trackInk := pal.text.Alpha(0.13)
	if hot {
		trackInk = pal.text.Alpha(0.26)
	}
	box := ui.Box(c).Size(usageRingSize, usageRingSize).Shrink(0)
	if mark == markBalance {
		sign := "$"
		if strings.HasPrefix(r.Balance, "CNY") {
			sign = "¥"
		}
		box.Center().Children(func() {
			ui.Text(c, sign).FontSize(10.5).FontWeight(600).TextColor(pal.textMuted).SingleLine()
		})
	}
	box.Draw(func(p *ui.Painter, rect ui.Rect) {
		cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
		rad := min(rect.W, rect.H)/2 - 1.5
		if dot {
			// Drawn last, over the ring's top right.
			defer func() {
				var d ui.Path
				d.Circle(rect.X+rect.W-3.5, rect.Y+3.5, 2.5)
				p.FillPath(&d, pal.attention)
			}()
		}
		if mark == markUnconnected {
			// Eight dashes round a fainter track: nothing to read yet.
			const dashes = 8
			for i := range dashes {
				p.Transform(ui.Rotation(float32(i)*360/dashes), cx, cy, func() {
					var dash ui.Path
					dash.Circle(cx, cy, rad)
					p.StrokePath(dash.Trim(0, 0.5/dashes), 1.5, trackInk)
				})
			}
			return
		}
		var track ui.Path
		track.Circle(cx, cy, rad)
		p.StrokePath(&track, 2, trackInk)
		if used <= 0 || mark != markUsage {
			return
		}
		arc := pal.text
		if alert {
			arc = pal.attention
		}
		// From the top, clockwise: the circle starts at 3 o'clock.
		p.Transform(ui.Rotation(-90), cx, cy, func() {
			var path ui.Path
			path.Circle(cx, cy, rad)
			p.StrokePath(path.Trim(0, max(0.02, used/100)), 2, arc)
		})
	})
}

// usagePlan is a plan's name as the card and the modal show it: the
// providers' own are lowercase ("max", "plus", "pro"), which reads as a
// word, so they are capitalized, a word at a time.
func usagePlan(plan string) string {
	if plan != strings.ToLower(plan) {
		return plan
	}
	words := strings.Fields(strings.ReplaceAll(plan, "_", " "))
	for i, w := range words {
		r, n := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToTitle(r)) + w[n:]
	}
	return strings.Join(words, " ")
}

// usageWindow is a window's name as the card says it, shortly: a week.
func usageWindow(label string) string {
	if label == "Weekly" {
		return "Week"
	}
	return label
}

// usageHover shows a harness's full card above its ring: the plan, each
// window with its reset, a balance, or why it is not connected.
func (a *App) usageHover(c *ui.Context, pal *palette, r usage.Report, anchor ui.Element) {
	ui.Overlay(c, func() {
		card := ui.Column(c.Key("usage-hover")).Width(240).Padding(11, 12).Gap(7).Radius(10).Background(pal.popover)
		if pal.dark {
			floating(card, pal)
		} else {
			floating(card.Border(1, pal.cardBorder), pal)
		}
		card.AttachTo(anchor, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, 6, 0)
		a.usage.cardOver = card.Hovered()
		card.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, r.Name).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
				ui.Spacer(c)
				if r.Plan != "" {
					ui.Text(c, usagePlan(r.Plan)).FontSize(11).TextColor(pal.textFaint).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				}
			})
			for i, w := range r.Windows {
				ui.Row(c.Key(fmt.Sprint(i))).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Text(c, usageWindow(w.Label)).FontSize(11).TextColor(pal.textMuted).SingleLine().Shrink(0)
					ui.Spacer(c)
					ui.Text(c, fmt.Sprintf("%.0f%%", w.Used)).FontSize(11.5).FontWeight(600).TextColor(pal.text).Shrink(0)
					if !w.Resets.IsZero() {
						ui.Text(c, "· "+usageReset(w.Resets)).FontSize(11).TextColor(pal.textFaint).SingleLine().Shrink(0)
					}
				})
			}
			if r.Balance != "" {
				ui.Text(c, r.Balance+" remaining").FontSize(12).TextColor(pal.text)
			}
			switch {
			case r.Error != "" && !r.Checked.IsZero():
				// What shows is the last read that worked.
				ui.Text(c, locales.UsageStale()).FontSize(11).TextColor(pal.attention).SingleLine().Ellipsis("…")
				ui.Text(c, r.Error).FontSize(11).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
			case r.Error != "":
				ui.Text(c, r.Error).FontSize(11).TextColor(pal.attention).SingleLine().Ellipsis("…")
			case r.Checked.IsZero():
				ui.Text(c, usageSummary(r, a.usage.loading)).FontSize(11).TextColor(pal.textMuted)
			}
		})
	})
}

// usageReset is a window's reset as the card says it: the time today, the
// weekday for a later day this week, the date from six days on, when the
// weekday would read as today's, or now when it is due.
func usageReset(at time.Time) string {
	return usageResetAt(at, time.Now())
}

func usageResetAt(at, now time.Time) string {
	if !at.After(now) {
		return "resets now"
	}
	local, now := at.Local(), now.Local()
	if local.Format("2006-01-02") == now.Format("2006-01-02") {
		return "resets " + local.Format("3:04 PM")
	}
	if at.Sub(now) < 6*24*time.Hour && local.Weekday() != now.Weekday() {
		return "resets " + local.Format("Mon")
	}
	return "resets " + local.Format("Jan 2")
}

func (a *App) openUsage() {
	if a.usage.open || a.confirm != nil {
		return
	}
	a.pal.open = false
	a.usage.open = true
	a.usage.keys = map[string]*string{}
	a.usage.hover, a.usage.cardOver = "", false
}

func (a *App) closeUsage() {
	a.usage.open = false
	a.usage.keys = nil
	a.usage.notice = ""
	a.focusTerminal()
}

func (a *App) usageModal(c *ui.Context, pal *palette) {
	if !a.usage.open {
		return
	}
	w, h := c.Size()
	ui.DialogBase(c.Key("usage-modal"), &a.usage.open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.overlay)
		floating(panel.Width(min(640, w-32)).Height(min(720, h-40)).Radius(14).Background(pal.popover), pal).Clip().Label("Account usage")
		if panel.OverlayShortcut(ui.Cmd, ui.KeyR) {
			a.refreshUsage()
		}
		a.usageContent(c, pal)
	})
	if !a.usage.open {
		a.closeUsage()
	}
}

func (a *App) usageContent(c *ui.Context, pal *palette) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(64).Shrink(0).Padding(12, 20, 0, 24).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Account usage").FontSize(20).Bold().TextColor(pal.text)
			ui.Spacer(c)
			label := "Refresh"
			if a.usage.loading {
				label = "Checking…"
			}
			if ui.Button(c, label).Disabled(a.usage.loading).Clicked() {
				a.refreshUsage()
			}
			iconButton(c, "close-usage", closeIcon, "Close usage (Esc)", pal, a.closeUsage)
		})
		ui.Scroll(c.Key("usage-scroll")).Grow(1).MinHeight(0).TrackScroll(&a.usage.scroll).Padding(12, 24, 24, 24).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				ui.Text(c, "Your account allowances, across projects.").FontSize(12).TextColor(pal.textMuted)
				a.resourceCard(c, pal)
				for _, r := range a.usageReports() {
					a.usageCard(c, pal, r)
				}
				if a.usage.notice != "" {
					ui.Text(c, a.usage.notice).FontSize(12).TextColor(pal.textMuted)
				}
				ui.Text(c, "Updates every 2 minutes. Account limits come from each provider; they are separate from a session’s context window.").FontSize(11).TextColor(pal.textFaint)
			})
		})
	})
	c.After(time.Minute)
}

func resetLabel(at time.Time) string {
	if at.IsZero() {
		return "Reset time unavailable"
	}
	if time.Now().After(at) {
		return "Reset due · refresh to check"
	}
	return "Resets " + at.Local().Format("Mon 3:04 PM")
}

func (a *App) usageCard(c *ui.Context, pal *palette, r usage.Report) {
	ui.Column(c.Key(r.ID)).Padding(16).Gap(10).Radius(12).Background(pal.card).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, r.Name).FontSize(14).Bold().TextColor(pal.text)
			if r.Plan != "" {
				ui.Text(c, usagePlan(r.Plan)).FontSize(11).TextColor(pal.textFaint)
			}
			ui.Spacer(c)
			if ui.Button(c, "Open account ↗").Clicked() {
				c.OpenURL(r.URL)
			}
		})
		for i, w := range r.Windows {
			ui.Column(c.Key(fmt.Sprint(i))).Gap(5).Children(func() {
				ui.Row(c).Children(func() {
					ui.Text(c, w.Label).FontSize(12).TextColor(pal.textMuted)
					ui.Spacer(c)
					ui.Text(c, fmt.Sprintf("%.0f%% used", w.Used)).FontSize(12).TextColor(pal.text)
				})
				col := pal.accent
				if w.Used >= 90 {
					col = pal.attention
				}
				ui.Box(c).Height(4).Radius(2).Background(pal.divider).Children(func() {
					ui.Box(c).Height(4).WidthPercent(float32(w.Used)).Radius(2).Background(col)
				})
				ui.Text(c, resetLabel(w.Resets)).FontSize(10.5).TextColor(pal.textFaint)
			})
		}
		if r.Balance != "" {
			ui.Text(c, r.Balance+" remaining").FontSize(16).TextColor(pal.text)
		}
		if r.Error != "" {
			ui.Text(c, r.Error).FontSize(12).TextColor(pal.attention)
		} else if r.Checked.IsZero() {
			ui.Text(c, usageSummary(r, a.usage.loading)).FontSize(12).TextColor(pal.textMuted)
		}
		if !r.Checked.IsZero() {
			prefix := "Updated "
			if r.Error != "" {
				prefix = "Last successful update "
			}
			ui.Text(c, prefix+r.Checked.Local().Format("Mon 3:04 PM")).FontSize(10.5).TextColor(pal.textFaint)
		}
		if r.Checked.IsZero() || r.Error != "" {
			help := map[string]string{
				"claude":   "Reads your Claude Code sign-in. Run claude and sign in, then refresh.",
				"codex":    "Reads your Codex sign-in, or OpenCode’s OpenAI login. Run codex login, then refresh.",
				"opencode": "Reads your OpenCode workspace key. If it expired, sign in through OpenCode or enter a replacement key below.",
				"deepseek": "Direct DeepSeek API credit. For DeepSeek through OpenCode Go, use the OpenCode allowance instead.",
			}[r.ID]
			ui.Text(c, help).FontSize(11).TextColor(pal.textMuted)
		}
		if r.ID == "opencode" || r.ID == "deepseek" {
			if a.usage.keys == nil {
				a.usage.keys = map[string]*string{}
			}
			key := a.usage.keys[r.ID]
			if key == nil {
				key = new(string)
				a.usage.keys[r.ID] = key
			}
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.TextInput(c.Key("key"), key).Password().Label(r.Name + " API key").Placeholder("API key (optional)").Grow(1).MinWidth(0)
				if ui.Button(c, "Save key").Disabled(a.usage.saving || a.usage.loading || strings.TrimSpace(*key) == "").Clicked() {
					a.saveUsageKey(r.ID, *key)
				}
			})
			ui.Text(c, "Saved on this Mac in Shibu Code’s private app folder.").FontSize(10).TextColor(pal.textFaint)
		}
	})
}

func (a *App) saveUsageKey(id, key string) {
	if a.usage.saving {
		return
	}
	a.usage.saving = true
	go func() {
		err := usage.SaveKey(id, key)
		a.update(func() {
			a.usage.saving = false
			if err != nil {
				a.usage.notice = err.Error()
			} else {
				if key := a.usage.keys[id]; key != nil {
					*key = ""
				}
				a.usage.notice = "API key saved."
				a.refreshUsage()
			}
			a.invalidateUsage()
		})
	}()
}
