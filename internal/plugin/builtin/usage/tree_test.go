package usage

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	accountusage "kero/internal/usage"
	"kero/internal/usage/cost"
	"kero/locales"
)

var treeNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// The widget shows local times in production. Give the package's fixtures
// one local zone before tests start, so snapshots agree on every machine
// without changing time.Local while plugin worker goroutines run.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// outline writes a tree as one line for each node, so a golden file shows
// what changed.
func outline(n *protocol.Node, depth int, b *strings.Builder) {
	fmt.Fprintf(b, "%s%s %s", strings.Repeat("  ", depth), n.Type, n.ID)
	if n.Text != "" {
		fmt.Fprintf(b, " %q", n.Text)
	}
	if n.Icon != "" {
		fmt.Fprintf(b, " icon=%s", n.Icon)
	}
	if n.Mark != "" {
		fmt.Fprintf(b, " mark=%s", n.Mark)
	}
	if n.Color != "" {
		fmt.Fprintf(b, " color=%s", n.Color)
	}
	if n.Surface != "" {
		fmt.Fprintf(b, " surface=%s", n.Surface)
	}
	if n.Type == protocol.NodeProgress {
		fmt.Fprintf(b, " value=%.3f", n.Value)
		if n.Marker != nil {
			fmt.Fprintf(b, " marker=%.3f", *n.Marker)
		}
	}
	for _, s := range n.Segments {
		fmt.Fprintf(b, " seg(%s=%.2f,%s)", s.ID, s.Value, s.Color)
	}
	for _, it := range n.Items {
		fmt.Fprintf(b, " item(%s,%q,%v)", it.ID, it.Title, it.Active)
	}
	b.WriteString("\n")
	for _, c := range n.Children {
		outline(c, depth+1, b)
	}
}

func TestUsageLargeTreeSnapshot(t *testing.T) {
	v := testView(protocol.Size{Width: 640, Height: 480}, treeNow)
	v.open = map[string]bool{"codex": true}
	tree := widgetTree(v, treeNow)
	mustValid(t, tree)
	var b strings.Builder
	outline(tree.Root, 0, &b)

	golden := filepath.Join("testdata", "large_tree.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(golden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("no golden tree (UPDATE_GOLDEN=1 writes it): %v", err)
	}
	if b.String() != string(want) {
		t.Errorf("the large tree changed (UPDATE_GOLDEN=1 accepts it):\n%s", b.String())
	}
}

func TestUsageLargeTreeIDs(t *testing.T) {
	tree := widgetTree(testView(protocol.Size{Width: 640, Height: 480}, treeNow), treeNow)
	mustValid(t, tree)
	for _, id := range []string{
		"usage-root", "usage-cost", "usage-range", "usage-cost-body", "usage-cost-total", "usage-legend",
		"leg-claude", "leg-dot-claude", "leg-name-claude", "leg-usd-claude",
		"sec-claude", "head-claude", "name-claude", "plan-claude", "toggle-claude", "card-claude",
		"win-claude-0", "bar-claude-0", "wlabel-claude-1", "wleft-claude-0", "wreset-claude-0",
		"usage-foot", "usage-updated", "usage-refresh",
		"wait-deepseek", "help-deepseek", "error-deepseek",
	} {
		if !hasNode(tree, id) && id != "wait-deepseek" {
			t.Errorf("no node %q", id)
		}
	}
	for _, id := range []string{"usage-donut", "usage-system", "usage-cpu", "usage-ram", "usage-system-title", "cost-today-claude"} {
		if hasNode(tree, id) {
			t.Errorf("unexpected node %q", id)
		}
	}
	if !tree.Scroll {
		t.Error("the large form does not scroll")
	}
	if got := treeNode(t, tree, "usage-cost-total"); got.Text != "≈$4,600.75" {
		t.Errorf("the total says %q / %q", got.Text, got.Detail)
	}
	if got := treeNode(t, tree, "leg-usd-claude"); got.Text != "$3,200.50" {
		t.Errorf("the Claude legend says %q", got.Text)
	}
	if got := treeNode(t, tree, "leg-usd-codex"); got.Text != "$1,400.25" {
		t.Errorf("the Codex legend says %q", got.Text)
	}
	if got := treeNode(t, tree, "leg-approx-codex"); got.Text != "≈" || got.Color != protocol.ColorMuted {
		t.Errorf("the Codex estimate sign is %q in %q", got.Text, got.Color)
	}
	if hasNode(tree, "leg-approx-claude") {
		t.Error("an exact amount has an estimate sign")
	}
	for _, id := range []string{"leg-name-claude", "leg-name-codex"} {
		if got := treeNode(t, tree, id); got.Mark != "" {
			t.Errorf("legend row %q has the mark %q", id, got.Mark)
		}
	}
	if got := treeNode(t, tree, "usage-estimated"); got.Text != locales.UsagePluginEstimatedShort() || got.Lines != 1 || !strings.Contains(got.Label, "gpt-5-codex") {
		t.Errorf("the estimate caption is %q (%d lines) with label %q", got.Text, got.Lines, got.Label)
	}
	if got := treeNode(t, tree, "usage-refresh"); got.Label != locales.UsagePluginRefresh() {
		t.Errorf("the refresh button is labeled %q", got.Label)
	}
	if got := treeNode(t, tree, "bar-claude-0"); !strings.Contains(got.Label, "58%") {
		t.Errorf("the weekly bar is labeled %q", got.Label)
	}
	if !hasNode(tree, "usage-estimated") {
		t.Error("estimates have no caption")
	}
	if got := treeNode(t, tree, "wlabel-claude-1"); got.Text != locales.UsagePluginSession() {
		t.Errorf("the five hours are %q", got.Text)
	}
	if bar := treeNode(t, tree, "bar-claude-0"); math.Abs(bar.Value-0.42) > 1e-9 || bar.Marker == nil {
		t.Errorf("the weekly bar is %v with marker %v", bar.Value, bar.Marker)
	}
	if got := treeNode(t, tree, "bar-codex-0"); got.Color != protocol.ColorDanger {
		t.Errorf("a nearly spent window is %q", got.Color)
	}
	if got := treeNode(t, tree, "wreset-claude-1").Text; got != "Resets in 2h" {
		t.Errorf("the reset says %q", got)
	}
}

func TestUsageLegendSortedByCost(t *testing.T) {
	v := testView(protocol.Size{Width: 640, Height: 480}, treeNow)
	v.costs = map[cost.Range][]cost.Spend{cost.Today: {
		{Provider: "opencode", USD: 5, Tokens: 1},
		{Provider: "claude", USD: 50, Tokens: 1},
		{Provider: "codex", USD: 20, Tokens: 1},
	}}
	tree := widgetTree(v, treeNow)
	var order []string
	for _, c := range treeNode(t, tree, "usage-legend").Children {
		order = append(order, c.ID)
	}
	if strings.Join(order, ",") != "leg-claude,leg-codex,leg-opencode" {
		t.Errorf("the legend is %v", order)
	}
}

func TestUsageRangesAndDetails(t *testing.T) {
	v := testView(protocol.Size{Width: 640, Height: 480}, treeNow)
	v.rng = rangeYesterday
	tree := widgetTree(v, treeNow)
	if got := treeNode(t, tree, "usage-cost-total").Text; got != "$31.20" {
		t.Errorf("yesterday's total says %q", got)
	}
	if hasNode(tree, "usage-estimated") {
		t.Error("an exact range has an estimate caption")
	}
	if !treeNode(t, tree, "usage-range").Items[1].Active {
		t.Error("yesterday is not the active range")
	}

	v.open = map[string]bool{"claude": true, "deepseek": true}
	tree = widgetTree(v, treeNow)
	mustValid(t, tree)
	if got := treeNode(t, tree, "toggle-claude").Icon; got != "chevron-up" {
		t.Errorf("an open section has %q", got)
	}
	if got := treeNode(t, tree, "cost-today-claude-value").Text; got != "$3.2K · 1.1B tokens" {
		t.Errorf("today's cost says %q", got)
	}
	if got := treeNode(t, tree, "cost-30d-deepseek-value").Text; got != locales.UsagePluginNoData() {
		t.Errorf("no cost says %q", got)
	}
}

func TestUsageNoCostLogs(t *testing.T) {
	v := testView(protocol.Size{Width: 640, Height: 480}, treeNow)
	v.costs = map[cost.Range][]cost.Spend{}
	tree := widgetTree(v, treeNow)
	mustValid(t, tree)
	if got := treeNode(t, tree, "usage-cost-total"); got.Text != locales.UsagePluginNoValue() || len(got.Segments) != 0 {
		t.Errorf("an empty total says %q with %d segments", got.Text, len(got.Segments))
	}
	if got := treeNode(t, tree, "usage-nocost").Text; got != locales.UsagePluginNoCostLogs() {
		t.Errorf("the caption says %q", got)
	}
	v.costLoaded = false
	if got := treeNode(t, widgetTree(v, treeNow), "usage-nocost").Text; got != locales.UsagePluginChecking() {
		t.Errorf("before the first read the caption says %q", got)
	}
}

func TestUsageStackedBody(t *testing.T) {
	tree := widgetTree(testView(protocol.Size{Width: 320, Height: 480}, treeNow), treeNow)
	mustValid(t, tree)
	if got := treeNode(t, tree, "usage-cost-body").Type; got != protocol.NodeColumn {
		t.Errorf("a narrow pane has a %s body", got)
	}
	tree = widgetTree(testView(protocol.Size{Width: 640, Height: 480}, treeNow), treeNow)
	if got := treeNode(t, tree, "usage-cost-body").Type; got != protocol.NodeColumn {
		t.Errorf("a wide pane has a %s body", got)
	}
}

func TestUsageAccountStates(t *testing.T) {
	reports := []accountusage.Report{
		{ID: "claude", Name: "Claude", Windows: []accountusage.Window{{Label: "Weekly", Used: 42}}, Checked: treeNow.Add(-time.Hour), Error: "Rate limited"},
		{ID: "deepseek", Name: "DeepSeek", Balance: "$4.20", Checked: treeNow},
		{ID: "codex", Name: "Codex"},
	}
	v := &view{size: protocol.Size{Width: 640, Height: 480}, reports: reports, rng: rangeToday}
	tree := widgetTree(v, treeNow)
	mustValid(t, tree)
	if got := treeNode(t, tree, "bar-claude-0"); got.Color != protocol.ColorMuted {
		t.Errorf("a stale bar is %q", got.Color)
	}
	treeNode(t, tree, "stale-claude")
	treeNode(t, tree, "error-claude")
	if got := treeNode(t, tree, "balance-deepseek").Text; got != "$4.20 remaining" {
		t.Errorf("the balance says %q", got)
	}
	if hasNode(tree, "bar-deepseek-0") {
		t.Error("a balance has a bar")
	}
	treeNode(t, tree, "wait-codex")
	treeNode(t, tree, "help-codex")
	if hasNode(tree, "bar-codex-0") {
		t.Error("an account never read has a bar")
	}
}

func TestUsageCompactTree(t *testing.T) {
	tree := widgetTree(testView(protocol.Size{Width: 280, Height: 150}, treeNow), treeNow)
	mustValid(t, tree)
	if tree.Scroll {
		t.Error("the short form scrolls")
	}
	if got := treeNode(t, tree, "usage-total").Text; got != "≈$4.6K today" {
		t.Errorf("the head says %q", got)
	}
	for _, id := range []string{"usage-head", "usage-refresh", "row-claude", "row-claude-top", "name-claude", "sum-claude", "bar-claude", "row-deepseek"} {
		treeNode(t, tree, id)
	}
	for _, id := range []string{"usage-cost", "usage-donut", "usage-system", "usage-cpu", "bar-deepseek"} {
		if hasNode(tree, id) {
			t.Errorf("the short form has %q", id)
		}
	}
	// The worst window, the weekly 42%, leaves 58%; Claude's session is at 8%.
	if got := treeNode(t, tree, "sum-claude").Text; got != "58% left" {
		t.Errorf("the summary says %q", got)
	}
	if got := treeNode(t, tree, "sum-codex").Text; got != "10% left" {
		t.Errorf("the summary says %q", got)
	}
	if got := treeNode(t, tree, "bar-codex"); got.Color != protocol.ColorDanger {
		t.Errorf("a nearly spent bar is %q", got.Color)
	}
}

func TestUsageCompactChecksBeforeTheFirstRead(t *testing.T) {
	pending := []accountusage.Report{{ID: "claude", Name: "Claude"}}
	tree := widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: pending}, treeNow)
	mustValid(t, tree)
	if got := treeNode(t, tree, "usage-checking").Text; got != locales.UsagePluginChecking() {
		t.Errorf("the checking label says %q", got)
	}
	if got := treeNode(t, tree, "usage-title").Text; got != locales.UsagePluginTitle() {
		t.Errorf("without a cost the head says %q", got)
	}
	if hasNode(tree, "bar-claude") {
		t.Error("an account never read has a bar")
	}
}

func TestUsageCompactStaleAndBalance(t *testing.T) {
	reports := []accountusage.Report{
		{ID: "claude", Name: "Claude", Windows: []accountusage.Window{{Label: "Weekly", Used: 42}}, Checked: treeNow.Add(-time.Hour), Error: "Rate limited"},
		{ID: "deepseek", Name: "DeepSeek", Balance: "$4.20", Checked: treeNow},
	}
	tree := widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: reports}, treeNow)
	mustValid(t, tree)
	if sum := treeNode(t, tree, "sum-claude"); sum.Color != protocol.ColorMuted || sum.Text != "58% left" {
		t.Errorf("the stale summary is %q in %q", sum.Text, sum.Color)
	}
	if got := treeNode(t, tree, "stale-claude"); got.Text != locales.UsagePluginStale() {
		t.Errorf("the stale caption says %q", got.Text)
	}
	if got := treeNode(t, tree, "sum-deepseek").Text; got != "$4.20" {
		t.Errorf("the balance says %q", got)
	}
	if hasNode(tree, "bar-deepseek") {
		t.Error("a balance has a bar")
	}
}

func TestUsageCompactFlameOnProjectedLimit(t *testing.T) {
	reports := []accountusage.Report{{ID: "claude", Name: "Claude", Checked: treeNow,
		Windows: []accountusage.Window{{Label: "5 hours", Used: 80, Resets: treeNow.Add(4 * time.Hour)}}}}
	tree := widgetTree(&view{size: protocol.Size{Width: 280, Height: 150}, reports: reports}, treeNow)
	mustValid(t, tree)
	if got := treeNode(t, tree, "flame-claude"); got.Icon != "flame" {
		t.Errorf("the flame is %q", got.Icon)
	}
}

func TestUsageLayoutFor(t *testing.T) {
	for _, c := range []struct {
		size protocol.Size
		want layout
	}{
		{protocol.Size{}, layoutLarge},
		{protocol.Size{Width: 640, Height: 480}, layoutLarge},
		{protocol.Size{Width: 320, Height: 480}, layoutStacked},
		{protocol.Size{Width: 640, Height: 250}, layoutCompact},
		{protocol.Size{Width: 250, Height: 480}, layoutCompact},
	} {
		if got := layoutFor(c.size); got != c.want {
			t.Errorf("%v is layout %d, want %d", c.size, got, c.want)
		}
	}
}

// Spend of a provider with no account on this Mac is named, not shown by id.
func TestUsageLegendNamesProvidersWithoutAccounts(t *testing.T) {
	v := testView(protocol.Size{Width: 640, Height: 480}, treeNow)
	v.providers = append(testReports(treeNow), accountusage.Report{ID: "opencode", Name: "OpenCode Go"})
	v.costs = map[cost.Range][]cost.Spend{cost.Today: {{Provider: "opencode", USD: 5, Tokens: 1}}}
	tree := widgetTree(v, treeNow)
	if got := treeNode(t, tree, "leg-name-opencode").Text; got != "OpenCode Go" {
		t.Errorf("the legend names the provider %q", got)
	}
	if got := providerName(v, "mystery"); got != "mystery" {
		t.Errorf("an unknown provider is %q", got)
	}
}
