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
	loading bool
	ctx     context.Context
	cancel  context.CancelFunc
	open    bool
	scroll  ui.ScrollState
	keys    map[string]*string
	notice  string
	saving  bool
	// hover is the harness whose card shows, and cardOver tells the pointer
	// is on that card, which keeps it open as the pointer leaves the row.
	hover    string
	cardOver bool
}

func (a *App) startUsage() {
	a.usage.ctx, a.usage.cancel = context.WithCancel(context.Background())
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
		reports := usage.Refresh(ctx, previous)
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			a.usage.reports, a.usage.loading = reports, false
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
		return locales.UsagePercentUsed(fmt.Sprintf("%.0f", r.Windows[0].Used))
	}
	return "Unavailable"
}

// usageColor preserves the list's warning threshold and muted error state.
func usageColor(r usage.Report, pal *palette) ui.Color {
	if r.Error != "" {
		return pal.textFaint
	}
	for _, w := range r.Windows {
		if w.Used >= 90 {
			return pal.attention
		}
	}
	return pal.textMuted
}

// usageFooterSummary prefers USD in the compact list; the card retains the
// provider's full balance. No allowance is substituted for a missing report.
func usageFooterSummary(r usage.Report, loading bool) string {
	if !r.Checked.IsZero() && r.Balance != "" {
		for _, part := range strings.Split(r.Balance, "·") {
			if part = strings.TrimSpace(part); strings.HasPrefix(part, "USD ") {
				return part
			}
		}
	}
	return usageSummary(r, loading)
}

// usageSidebar is the original usage list, with the full allowance card
// available from each provider row as well as the account details dialog.
func (a *App) usageSidebar(c *ui.Context, pal *palette) {
	var anchor ui.Element
	var shown usage.Report
	var card bool
	ui.Column(c.Key("account-usage")).Width(a.prefs.SidebarWidth).Shrink(0).Padding(6, 12, 8, 12).Gap(1).Children(func() {
		header := ui.ButtonBase(c.Key("usage-details")).Height(24).Radius(5).Padding(0, 8).Label(locales.UsageAccountTitle()).Tooltip(a.tip(locales.UsageDetailsHint(), "usage.open"))
		if header.Hovered() {
			header.Background(pal.rowHover)
		}
		header.Children(func() {
			ui.Row(c).Fill().AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.UsageTitle()).FontSize(11).Bold().TextColor(pal.textMuted)
				ui.Spacer(c)
				ui.Text(c, a.keys("usage.open")).FontSize(10).TextColor(pal.textFaint)
			})
		})
		if header.Clicked() {
			a.openUsage()
		}
		for _, r := range a.usageReports() {
			row := a.usageRow(c, pal, r)
			if a.usage.hover == r.ID {
				anchor, shown, card = row, r, true
			}
		}
	})
	if card {
		a.usageHover(c, pal, shown, anchor)
	}
}

func (a *App) usageRow(c *ui.Context, pal *palette, r usage.Report) ui.Element {
	key := "usage-" + r.ID
	row := ui.ButtonBase(c.Key(key)).Height(25).Padding(0, 8).Radius(5).
		Label(r.Name + " usage").Description(usageSummary(r, a.usage.loading))
	if row.Hovered() {
		row.Background(pal.rowHover)
	}
	row.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, r.Name).FontSize(11.5).TextColor(pal.textMuted).SingleLine().Shrink(0)
			ui.Spacer(c)
			ui.Text(c, usageFooterSummary(r, a.usage.loading)).FontSize(11).TextColor(usageColor(r, pal)).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			if r.Error != "" && !r.Checked.IsZero() {
				ui.Row(c).Size(10, 10).Shrink(0).Label(locales.UsageStale()).Tooltip(locales.UsageStale()).Children(func() {
					symbolMark(c, "exclamationmark.circle.fill", 10, pal.attention, pal.dark)
				})
			}
		})
	})
	if row.Clicked() {
		a.openUsage()
	}
	over := row.Hovered() || a.usage.hover == r.ID && a.usage.cardOver
	stays := a.hoverIntent(c, key, over)
	switch {
	case row.Hovered():
		a.usage.hover = r.ID
	case a.usage.hover == r.ID && !stays:
		a.usage.hover, a.usage.cardOver = "", false
	}
	return row
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

// usageHover shows a provider's full card above its row: the plan, each
// window with its reset, a balance, or why it is not connected.
func (a *App) usageHover(c *ui.Context, pal *palette, r usage.Report, anchor ui.Element) {
	ui.Overlay(c, func() {
		card := ui.Column(c.Key("usage-hover")).Width(232).Padding(14, 14).Gap(6).Radius(12).Background(pal.popover)
		if pal.dark {
			floating(card, pal)
		} else {
			floating(card.Border(1, pal.cardBorder), pal)
		}
		card.AttachTo(anchor, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, 6, 0)
		a.usage.cardOver = card.Hovered()
		card.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, r.Name).FontSize(14).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
				ui.Spacer(c)
				if r.Plan != "" {
					ui.Text(c, usagePlan(r.Plan)).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				}
			})
			var resetWidth float32
			for _, w := range r.Windows {
				if !w.Resets.IsZero() {
					width, _ := c.MeasureText(0, ui.Span{Text: "· " + usageReset(w.Resets), Size: 12})
					resetWidth = max(resetWidth, width)
				}
			}
			for i, w := range r.Windows {
				ui.Row(c.Key(fmt.Sprint(i))).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Text(c, usageWindow(w.Label)).FontSize(12.5).TextColor(pal.textMuted).SingleLine().Shrink(0)
					ui.Spacer(c)
					ui.Text(c, fmt.Sprintf("%.0f%%", w.Used)).Width(38).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
					if resetWidth > 0 {
						reset := ""
						if !w.Resets.IsZero() {
							reset = "· " + usageReset(w.Resets)
						}
						ui.Text(c, reset).Width(resetWidth).FontSize(12).TextColor(pal.textMuted).SingleLine().Shrink(0)
					}
				})
			}
			if r.Balance != "" {
				ui.Text(c, locales.UsageBalanceRemaining(r.Balance)).FontSize(12).TextColor(pal.text)
			}
			switch {
			case r.Error != "" && !r.Checked.IsZero():
				// What shows is the last read that worked.
				ui.Text(c, locales.UsageStale()).FontSize(11).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
				ui.Text(c, r.Error).FontSize(11).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
			case r.Error != "":
				ui.Text(c, r.Error).FontSize(11).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
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
		floating(panel.Width(min(640, w-32)).Height(min(720, h-40)).Radius(14).Background(pal.popover), pal).Clip().Label(locales.UsageAccountTitle())
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
			ui.Text(c, locales.UsageAccountTitle()).FontSize(20).Bold().TextColor(pal.text)
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
			ui.Text(c, locales.UsageBalanceRemaining(r.Balance)).FontSize(16).TextColor(pal.text)
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
