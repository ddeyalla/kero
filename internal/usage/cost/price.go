package cost

import (
	"sort"
	"strings"
)

// Price is USD per million tokens.
type Price struct {
	In  float64 // uncached input
	Out float64 // output, reasoning included
	CW5 float64 // cache write, 5 minute TTL
	CW1 float64 // cache write, 1 hour TTL
	CR  float64 // cache read
}

// Quality says how far a price can be trusted.
type Quality int

const (
	// Unknown models count tokens and add no dollars.
	Unknown Quality = iota
	// Estimated models use a proxy price of a similar model, because the real
	// price is not known. The UI should say "estimate" for them.
	Estimated
	// Listed models use a published price.
	Listed
)

// The table below is an ESTIMATE, last checked 2026-10-22. Claude rows come
// from the cached Anthropic price list of 2026-10-06 (input and output); cache
// writes are 1.25x (5 minute) and 2x (1 hour) of input. Cache reads follow the
// published figure where one was found (Fable $0.25, Opus 5.5 and Sonnet $0.20)
// and 0.1x of input otherwise. The Haiku 5.5 cache read is a guess, and its
// price above 100K prompt tokens ($0.50 / $2.50) is not modeled. OpenAI and
// DeepSeek rows are proxies, not published prices. Fast mode, long-context
// surcharges and batch discounts are not modeled.
//
// Matching is by the longest prefix of the normalized model id (lower case,
// vendor prefix removed, dots turned into dashes), so dated variants resolve.
type priceRow struct {
	prefix string
	price  Price
	proxy  bool
}

var priceRows = []priceRow{
	// Claude Fable and Mythos.
	{prefix: "claude-fable-5", price: Price{10, 50, 12.5, 20, 0.25}},
	{prefix: "claude-mythos-5", price: Price{10, 50, 12.5, 20, 0.25}},
	// Claude Opus.
	{prefix: "claude-opus-5-5", price: Price{4, 20, 5, 8, 0.20}},
	{prefix: "claude-opus-5", price: Price{5, 25, 6.25, 10, 0.50}},
	{prefix: "claude-opus-4-5", price: Price{5, 25, 6.25, 10, 0.50}},
	{prefix: "claude-opus-4-6", price: Price{5, 25, 6.25, 10, 0.50}},
	{prefix: "claude-opus-4-7", price: Price{5, 25, 6.25, 10, 0.50}},
	{prefix: "claude-opus-4-8", price: Price{5, 25, 6.25, 10, 0.50}},
	{prefix: "claude-opus-4-1", price: Price{15, 75, 18.75, 30, 1.50}},
	{prefix: "claude-opus-4", price: Price{15, 75, 18.75, 30, 1.50}},
	{prefix: "claude-3-opus", price: Price{15, 75, 18.75, 30, 1.50}},
	// Claude Sonnet.
	{prefix: "claude-sonnet-5", price: Price{2, 10, 2.5, 4, 0.20}},
	{prefix: "claude-sonnet-4", price: Price{3, 15, 3.75, 6, 0.30}},
	{prefix: "claude-3-7-sonnet", price: Price{3, 15, 3.75, 6, 0.30}},
	{prefix: "claude-3-5-sonnet", price: Price{3, 15, 3.75, 6, 0.30}},
	// Claude Haiku.
	{prefix: "claude-haiku-5", price: Price{0.10, 0.50, 0.125, 0.20, 0.01}},
	{prefix: "claude-haiku-4", price: Price{1, 5, 1.25, 2, 0.10}},
	{prefix: "claude-3-5-haiku", price: Price{0.80, 4, 1, 1.6, 0.08}},
	{prefix: "claude-3-haiku", price: Price{0.25, 1.25, 0.30, 0.50, 0.03}},
	// OpenAI and Codex models: proxy prices.
	{prefix: "gpt-", price: Price{In: 1.25, Out: 10, CR: 0.125}, proxy: true},
	{prefix: "codex-", price: Price{In: 1.25, Out: 10, CR: 0.125}, proxy: true},
	{prefix: "gpt-6-luna", price: Price{In: 0.25, Out: 2, CR: 0.025}, proxy: true},
	{prefix: "gpt-5-6-luna", price: Price{In: 0.25, Out: 2, CR: 0.025}, proxy: true},
	// DeepSeek: proxy prices.
	{prefix: "deepseek", price: Price{In: 0.14, Out: 0.28, CR: 0.014}, proxy: true},
	{prefix: "deepseek-v4-pro", price: Price{In: 0.435, Out: 0.87, CR: 0.0435}, proxy: true},
}

func init() {
	// Longest prefix first, so the first match is the most specific one.
	sort.SliceStable(priceRows, func(i, j int) bool {
		return len(priceRows[i].prefix) > len(priceRows[j].prefix)
	})
}

// normalize lower-cases a model id and removes a vendor prefix such as
// "anthropic/" or "openrouter/openai/".
func normalize(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return strings.ReplaceAll(m, ".", "-")
}

// Lookup returns the price of a model and how far to trust it.
func Lookup(model string) (Price, Quality) {
	m := normalize(model)
	for _, row := range priceRows {
		if strings.HasPrefix(m, row.prefix) {
			if row.proxy {
				return row.price, Estimated
			}
			return row.price, Listed
		}
	}
	return Price{}, Unknown
}

// providerOf names the provider that bills a model. Claude Code can route to
// other vendors, and those tokens belong to the vendor.
func providerOf(model, fallback string) string {
	m := normalize(model)
	switch {
	case strings.HasPrefix(m, "claude-"):
		return "claude"
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "codex-"):
		return "codex"
	case strings.HasPrefix(m, "deepseek"):
		return "deepseek"
	}
	return fallback
}
