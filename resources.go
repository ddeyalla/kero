package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/resources"
	"kero/locales"
)

// resourcePeriod is how often the app reads the Mac's CPU and RAM while
// its window has the focus, and resourceAway while it does not. A reading
// that changes the meters rebuilds and paints the window, which costs the
// app over a hundred wakeups. Ten-second reads bound these meter rebuilds
// to six a minute (docs/perf.md, BRU-225). The user's eyes are elsewhere while
// the window does not have the focus, so the meters then move twice a minute, and
// read again at once as it comes back.
const (
	resourcePeriod = 10 * time.Second
	resourceAway   = 30 * time.Second
)

// resourceWait is the wait for the next reading of the meters.
func resourceWait(focused bool) time.Duration {
	if focused {
		return resourcePeriod
	}
	return resourceAway
}

type resourceState struct {
	sample resources.Sample
	err    string
}

// resourcePace is what the reader of the meters, on a goroutine of its
// own, knows of the window: whether it has the focus, and wake, which a
// window that gains it sends on to read at once.
type resourcePace struct {
	focused atomic.Bool
	wake    chan struct{}
}

// windowFocused tells the reader of the meters that the window gained or
// lost the focus.
func (p *resourcePace) windowFocused(focused bool) {
	if p.focused.Swap(focused) != focused && focused {
		select {
		case p.wake <- struct{}{}:
		default:
		}
	}
}

func (a *App) startResources() {
	ctx := a.usage.ctx
	go func() {
		sampler, err := resources.NewSampler()
		if err != nil {
			a.update(func() { a.resources.err = err.Error() })
			return
		}
		// Every 10 s: on a Mac at work
		// the meters' whole percent and tenth of a GiB change with nearly
		// every read, and each change rebuilds the window, so the period
		// is what bounds those rebuilds, at 6 a minute, and 2 while the
		// window does not have the focus (resourceWait).
		timer := time.NewTimer(resourceWait(a.resourcePace.focused.Load()))
		defer timer.Stop()
		// A read rebuilds and paints the window only when what the app
		// shows of it changed: at rest the readings can move by fractions
		// that the labels and bars round away. shown is what the app shows, kept here so
		// that the comparison does not read the app's state off its
		// thread; at first it is nothing a reading shows.
		var shown resourceShown
		for {
			reading, err := sampler.Read()
			if ctx.Err() != nil {
				return
			}
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			if next := showResources(reading, errText); next != shown {
				shown = next
				a.update(func() {
					if err != nil {
						a.resources.err = errText
						return
					}
					a.resources = resourceState{sample: reading}
				})
			}
			timer.Reset(resourceWait(a.resourcePace.focused.Load()))
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			case <-a.resourcePace.wake:
			}
		}
	}()
}

// resourceShown is what the app shows of a reading: the labels of the
// sidebar's meters, its tooltip and the usage card, the bars' whole
// percents, and the error, if any. Two readings that show the same are
// equal.
type resourceShown struct {
	cpu, memory, err  string
	cpuBar, memoryBar float64
}

// showResources is what the app shows of reading, or of err where the read
// failed: the app then keeps its last reading, and shows the error.
func showResources(reading resources.Sample, err string) resourceShown {
	if err != "" {
		return resourceShown{err: err}
	}
	return resourceShown{
		cpu:       cpuLabel(reading),
		memory:    memoryLabel(reading, true),
		cpuBar:    cpuPercent(reading),
		memoryBar: memoryPercent(reading),
	}
}

// cpuPercent is the CPU bar's fill, in whole percents as its label says it.
func cpuPercent(s resources.Sample) float64 {
	if s.CPU == nil {
		return 0
	}
	return math.Round(*s.CPU)
}

// memoryPercent is the RAM bar's fill, in whole percents, so that a reading
// a few bytes apart fills it the same.
func memoryPercent(s resources.Sample) float64 {
	if s.MemoryTotal == 0 {
		return 0
	}
	return math.Round(float64(s.MemoryUsed) / float64(s.MemoryTotal) * 100)
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
	button := ui.ButtonBase(c.Key("system-resources")).Height(48).Shrink(0).Margin(4, 12, 7, 12).Padding(8).Radius(5).
		Label("System resources").Tooltip("This Mac · " + cpuLabel(s) + " · " + memoryLabel(s, true) + ". Across all apps; click for details.")
	if button.Hovered() {
		button.Background(pal.rowHover)
	}
	button.Children(func() {
		if a.resources.err != "" {
			ui.Text(c, "System stats unavailable").FontSize(10.5).TextColor(pal.textFaint)
			return
		}
		ui.Row(c).Fill().Gap(16).Children(func() {
			resourceMeter(c.Key("cpu"), pal, "CPU", strings.TrimPrefix(cpuLabel(s), "CPU "), cpuPercent(s), s.CPU != nil)
			resourceMeter(c.Key("memory"), pal, "RAM", strings.TrimPrefix(memoryLabel(s, false), "RAM "), memoryPercent(s), s.MemoryTotal > 0)
		})
	})
	if button.Clicked() {
		a.openUsage()
	}
}

func resourceMeter(c *ui.Context, pal *palette, name, value string, percent float64, available bool) {
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(7).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, name).FontSize(10.5).TextColor(pal.textFaint).Shrink(0)
			ui.Spacer(c)
			ui.Text(c, value).FontSize(10.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0)
		})
		fill := pal.textMuted
		if percent >= 90 {
			fill = pal.attention
		}
		ui.Box(c).Height(3).Radius(2).Background(pal.divider).Children(func() {
			if available && !math.IsNaN(percent) && !math.IsInf(percent, 0) {
				ui.Box(c).Height(3).WidthPercent(float32(max(0, min(100, percent)))).Radius(2).Background(fill)
			}
		})
	})
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
		ui.Text(c, locales.ResourceUpdateInfo()).FontSize(11).TextColor(pal.textMuted)
	})
}
