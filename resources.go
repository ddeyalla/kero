package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/resources"
)

type resourceState struct {
	sample resources.Sample
	err    string
}

func (a *App) startResources() {
	ctx := a.usage.ctx
	go func() {
		sampler, err := resources.NewSampler()
		if err != nil {
			a.update(func() { a.resources.err = err.Error() })
			return
		}
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			reading, err := sampler.Read()
			if ctx.Err() != nil {
				return
			}
			a.update(func() {
				if err != nil {
					a.resources.err = err.Error()
					return
				}
				a.resources = resourceState{sample: reading}
			})
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func cpuLabel(s resources.Sample) string {
	if s.CPU == nil {
		return "CPU —"
	}
	return fmt.Sprintf("CPU %.0f%%", *s.CPU)
}

func memoryLabel(s resources.Sample, total bool) string {
	if s.MemoryTotal == 0 {
		return "RAM —"
	}
	const gib = 1 << 30
	if total {
		return fmt.Sprintf("RAM %.1f / %.0f GiB", float64(s.MemoryUsed)/gib, float64(s.MemoryTotal)/gib)
	}
	return fmt.Sprintf("RAM %.1f GiB", float64(s.MemoryUsed)/gib)
}

func (a *App) resourceSidebar(c *ui.Context, pal *palette) {
	s := a.resources.sample
	label := cpuLabel(s) + "   ·   " + memoryLabel(s, false)
	if a.resources.err != "" {
		label = "System stats unavailable"
	}
	button := ui.ButtonBase(c.Key("system-resources")).Height(24).Shrink(0).Margin(0, 12, 3, 12).Padding(0, 8).Radius(5).
		Label("System resources").Tooltip("This Mac · " + cpuLabel(s) + " · " + memoryLabel(s, true) + ". Across all apps; click for details.")
	if button.Hovered() {
		button.Background(pal.rowHover)
	}
	button.Children(func() { ui.Text(c, label).FontSize(10.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…") })
	if button.Clicked() {
		a.openUsage()
	}
}

func (a *App) resourceCard(c *ui.Context, pal *palette) {
	s := a.resources.sample
	ui.Column(c.Key("machine-resources")).Padding(16).Gap(8).Radius(12).Background(pal.card).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "This Mac").FontSize(14).Bold().TextColor(pal.text)
			ui.Spacer(c)
			ui.Text(c, runtime.GOARCH+" · "+fmt.Sprint(runtime.NumCPU())+" cores").FontSize(11).TextColor(pal.textFaint)
		})
		ui.Row(c).Gap(24).Children(func() {
			ui.Text(c, cpuLabel(s)).FontSize(16).TextColor(pal.text)
			ui.Text(c, memoryLabel(s, true)).FontSize(16).TextColor(pal.text)
		})
		if a.resources.err != "" {
			ui.Text(c, a.resources.err).FontSize(11).TextColor(pal.attention)
		}
		ui.Text(c, "Across all apps. RAM includes active, wired and compressed memory; reclaimable cache is excluded. Updates every 2 seconds.").FontSize(11).TextColor(pal.textMuted)
	})
}
