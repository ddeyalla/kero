package usage

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	accountusage "kero/internal/usage"
	"kero/internal/usage/cost"
)

// fakeSource stands in for internal/usage and the cost package: a test
// decides what the accounts and the logs say, and counts the reads to see
// when the plugin makes them.
type fakeSource struct {
	reports   []accountusage.Report
	installed []string
	spends    map[cost.Range][]cost.Spend
	delay     time.Duration
	costDelay time.Duration

	mu            sync.Mutex
	refreshes     int
	costRefreshes int
	spendReads    int
	flushes       int
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

func (f *fakeSource) RefreshCost(ctx context.Context, now time.Time) error {
	f.mu.Lock()
	f.costRefreshes++
	delay := f.costDelay
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeSource) Spends(time.Time) map[cost.Range][]cost.Spend {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spendReads++
	return f.spends
}

func (f *fakeSource) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return nil
}

func (f *fakeSource) counts() (refreshes, costRefreshes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes, f.costRefreshes
}

func (f *fakeSource) spendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spendReads
}

// newFake is a source with accounts and a cost.
func newFake(now time.Time) *fakeSource {
	return &fakeSource{reports: testReports(now), spends: testSpends()}
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
				{Label: "5 hours", Used: 8, Resets: now.Add(2 * time.Hour)},
			},
			Checked: now,
		},
		{
			ID:   "codex",
			Name: "Codex",
			URL:  "https://chatgpt.com/codex/settings/usage",
			Plan: "plus",
			Windows: []accountusage.Window{
				{Label: "Weekly", Used: 90, Resets: now.Add(24 * time.Hour)},
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

// testSpends are three ranges of cost, Codex priced by a proxy.
func testSpends() map[cost.Range][]cost.Spend {
	return map[cost.Range][]cost.Spend{
		cost.Today: {
			{Provider: "claude", USD: 3200.5, Tokens: 1_100_000_000},
			{Provider: "codex", USD: 1400.25, Tokens: 22_100_000, Estimated: []string{"gpt-5-codex"}},
		},
		cost.Yesterday: {
			{Provider: "claude", USD: 31.2, Tokens: 950_000},
		},
		cost.Last30Days: {
			{Provider: "claude", USD: 41000, Tokens: 3_000_000_000},
			{Provider: "codex", USD: 1204.9, Tokens: 90_000_000, Unknown: []string{"mystery-1"}},
			{Provider: "opencode", USD: 12, Tokens: 1000},
		},
	}
}

// testView is a pane with the accounts and the cost already read.
func testView(size protocol.Size, now time.Time) *view {
	return &view{size: size, reports: testReports(now), costs: testSpends(), costLoaded: true, rng: rangeToday}
}

// addPane puts one pane into a plugin without a host.
func addPane(p *plugin, id string, size protocol.Size) {
	p.mu.Lock()
	p.panes[id] = &pane{id: id, size: size, rng: rangeToday, open: map[string]bool{}}
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

func hasNode(tree protocol.Tree, id string) bool {
	found := false
	treeWalk(tree.Root, func(n *protocol.Node) { found = found || n.ID == id })
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

func mustValid(t *testing.T, tree protocol.Tree) {
	t.Helper()
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
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
