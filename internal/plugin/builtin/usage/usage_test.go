package usage

import (
	"context"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/resources"
	accountusage "kero/internal/usage"
	"kero/locales"
)

// fakeSource stands in for internal/usage and internal/resources: a test
// decides what the accounts say, and counts the reads to see when the plugin
// makes them.
type fakeSource struct {
	reports   []accountusage.Report
	installed []string
	sample    resources.Sample
	sampleErr error
	delay     time.Duration

	mu        sync.Mutex
	refreshes int
	samples   int
	resets    int
}

func (f *fakeSource) Providers() []accountusage.Report { return f.reports }

func (f *fakeSource) Installed() []string { return f.installed }

func (f *fakeSource) Refresh(ctx context.Context, previous []accountusage.Report) []accountusage.Report {
	f.mu.Lock()
	f.refreshes++
	delay, reports := f.delay, f.reports
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
	}
	return reports
}

func (f *fakeSource) Sample() (resources.Sample, error) {
	f.mu.Lock()
	f.samples++
	sample, err := f.sample, f.sampleErr
	f.mu.Unlock()
	return sample, err
}

func (f *fakeSource) ResetSample() {
	f.mu.Lock()
	f.resets++
	f.mu.Unlock()
}

func (f *fakeSource) resetCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resets
}

func (f *fakeSource) counts() (refreshes, samples int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes, f.samples
}

// testReports is Claude with two windows and DeepSeek waiting for a sign-in.
func testReports(now time.Time) []accountusage.Report {
	return []accountusage.Report{
		{
			ID:   "claude",
			Name: "Claude",
			URL:  "https://claude.ai/settings/usage",
			Plan: "max",
			Windows: []accountusage.Window{
				{Label: "Weekly", Used: 42, Resets: now.Add(3 * 24 * time.Hour)},
				{Label: "Session", Used: 8, Resets: now.Add(2 * time.Hour)},
			},
			Checked: now,
		},
		{
			ID:    "deepseek",
			Name:  "DeepSeek",
			URL:   "https://platform.deepseek.com/usage",
			Error: "Not connected",
		},
	}
}

func testSample(now time.Time) resources.Sample {
	cpu := 23.0
	return resources.Sample{CPU: &cpu, MemoryUsed: 10 << 30, MemoryTotal: 32 << 30, At: now}
}

func testView(size protocol.Size, now time.Time) *view {
	return &view{size: size, reports: testReports(now), sample: testSample(now)}
}

// addPane puts one pane into a plugin without a host.
func addPane(p *plugin, id string, size protocol.Size) {
	p.mu.Lock()
	p.panes[id] = &pane{id: id, size: size}
	p.mu.Unlock()
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func treeWalk(n *protocol.Node, fn func(*protocol.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, child := range n.Children {
		treeWalk(child, fn)
	}
}

func treeNode(t *testing.T, tree protocol.Tree, id string) *protocol.Node {
	t.Helper()
	var found *protocol.Node
	treeWalk(tree.Root, func(n *protocol.Node) {
		if n.ID == id {
			found = n
		}
	})
	if found == nil {
		t.Fatalf("no node %q in the tree", id)
	}
	return found
}

func treeText(tree protocol.Tree) string {
	var lines []string
	treeWalk(tree.Root, func(n *protocol.Node) {
		if n.Text != "" {
			lines = append(lines, n.Text)
		}
		if n.Label != "" {
			lines = append(lines, n.Label)
		}
	})
	return strings.Join(lines, "\n")
}

// TestUsageCompactTree is the short form of the pane: one row for each
// account, with its bar and its percent, and the machine's two bars.
func TestUsageCompactTree(t *testing.T) {
	now := time.Now()
	tree := widgetTree(testView(protocol.Size{Width: 280, Height: 150}, now), now)
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}

	text := treeText(tree)
	for _, want := range []string{"Usage", "Claude", "DeepSeek", "42%", "Connect", "CPU", "23%", "RAM", "10.0 / 32 GiB"} {
		if !strings.Contains(text, want) {
			t.Errorf("the short form does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Resets") {
		t.Errorf("the short form shows a reset time:\n%s", text)
	}

	bar := treeNode(t, tree, "bar-claude")
	if math.Abs(bar.Value-0.42) > 1e-9 {
		t.Errorf("the Claude bar is %v, want 0.42", bar.Value)
	}
	treeNode(t, tree, "usage-refresh")
}

// TestUsageCompactChecksBeforeTheFirstRead is a narrow pane that opens before
// the accounts have been read: the head says it is checking, and a configured
// account waits instead of asking to connect.
func TestUsageCompactChecksBeforeTheFirstRead(t *testing.T) {
	now := time.Now()
	pending := []accountusage.Report{{ID: "claude", Name: "Claude", URL: "https://claude.ai/settings/usage"}}
	tree := widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: pending}, now)
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}

	text := treeText(tree)
	if !strings.Contains(text, locales.UsagePluginChecking()) {
		t.Errorf("the short form does not say it is checking:\n%s", text)
	}
	if strings.Contains(text, locales.UsagePluginConnect()) {
		t.Errorf("an account with no reading says connect:\n%s", text)
	}
	if got := treeNode(t, tree, "usage-checking"); got.Text != locales.UsagePluginChecking() {
		t.Errorf("the checking label says %q", got.Text)
	}

	// Once a report arrives, the short form keeps its usual summary.
	read := []accountusage.Report{{
		ID:      "claude",
		Name:    "Claude",
		Windows: []accountusage.Window{{Label: "Weekly", Used: 42}},
		Checked: now,
	}}
	text = treeText(widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: read}, now))
	if strings.Contains(text, locales.UsagePluginChecking()) {
		t.Errorf("the short form still says it is checking after a read:\n%s", text)
	}
	if !strings.Contains(text, "42%") {
		t.Errorf("the short form does not show the reading:\n%s", text)
	}
}

// TestUsageLargeTree is the full card: the plan, each window with its reset,
// the summary, and the machine with its note.
func TestUsageLargeTree(t *testing.T) {
	now := time.Now()
	tree := widgetTree(testView(protocol.Size{Width: 640, Height: 480}, now), now)
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}

	text := treeText(tree)
	for _, want := range []string{
		"Usage", "Claude", "Max", "Week", "42% used", "Resets",
		"Connect", "This Mac", "CPU", "23%", "RAM", "10.0 / 32 GiB",
		"Across all apps", "Account limits", "Direct DeepSeek API credit",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the full form does not say %q:\n%s", want, text)
		}
	}

	bar := treeNode(t, tree, "bar-claude-0")
	if math.Abs(bar.Value-0.42) > 1e-9 {
		t.Errorf("the Claude bar is %v, want 0.42", bar.Value)
	}
	reset := treeNode(t, tree, "reset-claude-0")
	if !strings.HasPrefix(reset.Text, "Resets ") {
		t.Errorf("the reset says %q, want a reset time", reset.Text)
	}
}

// TestUsageDoesNotRefreshWhileHidden is the rule the pane keeps: a pane that
// was never shown reads nothing.
func TestUsageDoesNotRefreshWhileHidden(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now())}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = 5*time.Millisecond, 5*time.Millisecond
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	p.setVisible(nil, "pane-1", false)
	time.Sleep(60 * time.Millisecond)

	if refreshes, samples := src.counts(); refreshes != 0 || samples != 0 {
		t.Fatalf("a hidden pane read: %d refreshes and %d samples", refreshes, samples)
	}
}

// TestUsageRefreshesOnlyWhileVisible shows the pane, sees it read, hides it,
// and checks that reading stops with it.
func TestUsageRefreshesOnlyWhileVisible(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now())}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = 10*time.Millisecond, 10*time.Millisecond
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the visible pane to read", func() bool {
		refreshes, samples := src.counts()
		return refreshes >= 2 && samples >= 2
	})

	p.setVisible(nil, "pane-1", false)
	p.work.Wait()
	refreshes, samples := src.counts()
	time.Sleep(80 * time.Millisecond)
	if got, gotSamples := src.counts(); got != refreshes || gotSamples != samples {
		t.Fatalf("a hidden pane kept reading: %d/%d refreshes, %d/%d samples", refreshes, got, samples, gotSamples)
	}
}

// TestUsageResumesAfterHideShow checks that a pane shown again reads again.
func TestUsageResumesAfterHideShow(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now())}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = 10*time.Millisecond, 10*time.Millisecond
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the first read", func() bool {
		refreshes, _ := src.counts()
		return refreshes >= 1
	})
	p.setVisible(nil, "pane-1", false)
	p.work.Wait()
	before, _ := src.counts()

	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the read after showing again", func() bool {
		refreshes, _ := src.counts()
		return refreshes > before
	})
	p.setVisible(nil, "pane-1", false)
	p.work.Wait()
}

// fakeClock is the plugin's clock in a test, moved by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// showAndHide shows a pane, lets it run a moment, and hides it again.
func showAndHide(p *plugin, id string) {
	p.setVisible(nil, id, true)
	time.Sleep(40 * time.Millisecond)
	p.setVisible(nil, id, false)
	p.work.Wait()
}

// TestUsageKeepsTheIntervalOverHideShow is a tab switch: a pane shown again
// within the interval does not read the accounts, one shown after it does,
// and the refresh button still reads them at once.
func TestUsageKeepsTheIntervalOverHideShow(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now())}
	clock := &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = time.Hour, time.Hour
	p.clock = clock.Now
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the first read", func() bool {
		refreshes, _ := src.counts()
		return refreshes == 1
	})
	p.setVisible(nil, "pane-1", false)
	p.work.Wait()

	clock.Add(30 * time.Minute)
	showAndHide(p, "pane-1")
	if refreshes, _ := src.counts(); refreshes != 1 {
		t.Fatalf("a pane shown again within the interval read the accounts: %d reads", refreshes)
	}

	clock.Add(31 * time.Minute)
	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the read after the interval", func() bool {
		refreshes, _ := src.counts()
		return refreshes == 2
	})
	waitFor(t, "the read to land", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !p.refreshing
	})

	p.event(nil, protocol.EventParams{Kind: protocol.KindPress, Node: "usage-refresh"})
	waitFor(t, "the read the button asks for", func() bool {
		refreshes, _ := src.counts()
		return refreshes == 3
	})
	p.setVisible(nil, "pane-1", false)
	p.work.Wait()
}

// TestUsageSlowReadKeepsSampling is a provider that takes its time: the
// machine's bars keep moving, and a press while the read is out does not
// start a second one.
func TestUsageSlowReadKeepsSampling(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now()), delay: time.Minute}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = time.Hour, 5*time.Millisecond
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the read to start", func() bool {
		refreshes, _ := src.counts()
		return refreshes == 1
	})
	p.event(nil, protocol.EventParams{Kind: protocol.KindPress, Node: "usage-refresh"})
	waitFor(t, "the machine to be sampled while the read is out", func() bool {
		_, samples := src.counts()
		return samples >= 4
	})
	if refreshes, _ := src.counts(); refreshes != 1 {
		t.Fatalf("a press while a read was out started another: %d reads", refreshes)
	}

	// Hiding the pane cancels the slow read, and the head stops checking.
	p.setVisible(nil, "pane-1", false)
	p.work.Wait()
	p.mu.Lock()
	refreshing, loading := p.refreshing, p.loading
	p.mu.Unlock()
	if refreshing || loading {
		t.Fatalf("a cancelled read left refreshing=%v loading=%v", refreshing, loading)
	}
}

// TestUsageResetsTheSamplerOnShow checks that each show starts the CPU
// sampler afresh, so its first reading is not an average over the hidden
// span.
func TestUsageResetsTheSamplerOnShow(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now())}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = time.Hour, time.Hour
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	showAndHide(p, "pane-1")
	showAndHide(p, "pane-1")
	if got := src.resetCount(); got != 2 {
		t.Fatalf("the sampler was reset %d times over two shows, want 2", got)
	}
}

// TestUsageCompactMarksStale is a refresh that failed after a reading: the
// short form mutes the last percent and says that it is the last known one.
func TestUsageCompactMarksStale(t *testing.T) {
	now := time.Now()
	reports := []accountusage.Report{{
		ID:      "claude",
		Name:    "Claude",
		Windows: []accountusage.Window{{Label: "Weekly", Used: 42}},
		Checked: now.Add(-time.Hour),
		Error:   "Rate limited",
	}}
	tree := widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: reports}, now)
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}
	if sum := treeNode(t, tree, "sum-claude"); sum.Text != "42%" || sum.Color != protocol.ColorMuted {
		t.Errorf("the stale summary is %q in %q, want a muted 42%%", sum.Text, sum.Color)
	}
	if got := treeNode(t, tree, "stale-claude"); got.Text != locales.UsagePluginStale() {
		t.Errorf("the stale caption says %q", got.Text)
	}

	reports[0].Error = ""
	tree = widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: reports}, now)
	if strings.Contains(treeText(tree), locales.UsagePluginStale()) {
		t.Errorf("a fresh reading is marked stale:\n%s", treeText(tree))
	}
	if sum := treeNode(t, tree, "sum-claude"); sum.Color == protocol.ColorMuted {
		t.Error("a fresh summary is muted")
	}
}

// TestUsageCompactBalanceHasNoBar is a provider with a balance and no
// window: the short form shows the balance without an empty bar.
func TestUsageCompactBalanceHasNoBar(t *testing.T) {
	now := time.Now()
	reports := []accountusage.Report{{ID: "deepseek", Name: "DeepSeek", Balance: "$4.20", Checked: now}}
	tree := widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: reports}, now)
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}
	if got := treeNode(t, tree, "sum-deepseek"); got.Text != "$4.20" {
		t.Errorf("the balance says %q", got.Text)
	}
	treeWalk(tree.Root, func(n *protocol.Node) {
		if n.ID == "bar-deepseek" {
			t.Error("a balance-only provider has a bar")
		}
	})
}

// TestUsageCloseStops checks that a closed pane stops the reads.
func TestUsageCloseStops(t *testing.T) {
	src := &fakeSource{reports: testReports(time.Now()), sample: testSample(time.Now())}
	p := newPlugin(src)
	p.usageEvery, p.sampleEvery = 10*time.Millisecond, 10*time.Millisecond
	addPane(p, "pane-1", protocol.Size{Width: 300, Height: 200})

	p.setVisible(nil, "pane-1", true)
	waitFor(t, "the first read", func() bool {
		refreshes, _ := src.counts()
		return refreshes >= 1
	})
	p.close(nil, "pane-1")
	p.work.Wait()
	refreshes, samples := src.counts()
	time.Sleep(80 * time.Millisecond)
	if got, gotSamples := src.counts(); got != refreshes || gotSamples != samples {
		t.Fatalf("a closed pane kept reading: %d/%d refreshes, %d/%d samples", refreshes, got, samples, gotSamples)
	}
}

// TestUsageFiltersInstalled keeps the accounts this Mac set up; an empty list
// means it could not tell, so every account stays.
func TestUsageFiltersInstalled(t *testing.T) {
	reports := testReports(time.Now())
	if got := filterReports(reports, nil); len(got) != len(reports) {
		t.Errorf("an unknown installed list dropped accounts: %d of %d", len(got), len(reports))
	}
	got := filterReports(reports, []string{"claude"})
	if len(got) != 1 || got[0].ID != "claude" {
		t.Errorf("the installed filter kept %v, want only Claude", got)
	}
}

// TestUsageManifestRegistered checks the manifest the app ships and that this
// build has the plugin's code.
func TestUsageManifestRegistered(t *testing.T) {
	builtins, err := registry.Builtins()
	if err != nil {
		t.Fatalf("the built-in registry failed: %v", err)
	}
	plugin, ok := registry.Find(builtins, "usage")
	if !ok {
		t.Fatal("no usage plugin in the built-in registry")
	}
	if !plugin.Builtin {
		t.Error("the usage plugin is not registered as built-in")
	}
	if plugin.Serve == nil {
		t.Error("the usage plugin has no code in this build")
	}
	m := plugin.Manifest
	if m.ID != "usage" || m.Name != "Usage" {
		t.Errorf("the manifest is %q/%q, want usage/Usage", m.ID, m.Name)
	}
	if len(m.Permissions) != 0 {
		t.Errorf("the usage plugin asks for permissions: %v", m.Permissions)
	}
	if len(m.Settings) != 0 {
		t.Errorf("the usage plugin has settings: %v", m.Settings)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("the manifest is invalid: %v", err)
	}
	if w := m.WidgetByName("usage"); w == nil || w.Title == "" {
		t.Errorf("the manifest has no usage widget: %+v", m.Widgets)
	}
}
