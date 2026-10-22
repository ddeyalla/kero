package usage

import (
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	accountusage "kero/internal/usage"
)

func TestUsageFormatters(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{money(1204.9), "$1,204.90"},
		{money(31.2), "$31.20"},
		{money(1234567.891), "$1,234,567.89"},
		{moneyShort(4600), "$4.6K"},
		{moneyShort(1_200_000), "$1.2M"},
		{moneyShort(31.2), "$31.20"},
		{moneyShort(2000), "$2K"},
		{moneyShort(999_960), "$1M"},
		{moneyShort(999_940), "$999.9K"},
		{moneyShort(999_960_000), "$1000M"},
		{tokens(999_960), "1M"},
		{tokens(999_940), "999.9K"},
		{tokens(999_960_000), "1B"},
		{tokens(999), "999"},
		{tokens(950), "950"},
		{tokens(950_000), "950K"},
		{tokens(22_100_000), "22.1M"},
		{tokens(1_100_000_000), "1.1B"},
		{duration(28 * time.Hour), "1d 4h"},
		{duration(24 * time.Hour), "1d"},
		{duration(4*time.Hour + 12*time.Minute), "4h 12m"},
		{duration(12 * time.Minute), "12m"},
		{duration(20 * time.Second), "<1m"},
		{approx(true, "$1.00"), "≈$1.00"},
		{approx(false, "$1.00"), "$1.00"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestUsageWindowPace(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name     string
		w        accountusage.Window
		marker   bool
		expected float64
		limit    bool
		hit      time.Duration
		tone     string
	}{
		{"half of five hours, on pace", accountusage.Window{Label: "5 hours", Used: 50, Resets: now.Add(150 * time.Minute)}, true, 0.5, false, 0, protocol.ColorBlue},
		{"fast use hits the limit", accountusage.Window{Label: "5 hours", Used: 80, Resets: now.Add(4 * time.Hour)}, true, 0.2, true, 15 * time.Minute, protocol.ColorDanger},
		{"too early to project", accountusage.Window{Label: "5 hours", Used: 30, Resets: now.Add(299 * time.Minute)}, true, 1.0 / 300, false, 0, protocol.ColorBlue},
		{"under 5 percent", accountusage.Window{Label: "Weekly", Used: 4, Resets: now.Add(3 * 24 * time.Hour)}, true, 4.0 / 7, false, 0, protocol.ColorBlue},
		{"nearly spent", accountusage.Window{Label: "Weekly", Used: 90, Resets: now.Add(time.Hour)}, true, 1 - 1.0/168, false, 0, protocol.ColorDanger},
		{"allowance has no length", accountusage.Window{Label: "Allowance", Used: 50, Resets: now.Add(time.Hour)}, false, 0, false, 0, protocol.ColorBlue},
		{"no reset time", accountusage.Window{Label: "Weekly", Used: 50}, false, 0, false, 0, protocol.ColorBlue},
		{"monthly", accountusage.Window{Label: "Monthly", Used: 10, Resets: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}, true, 20.5 / 31, false, 0, protocol.ColorBlue},
	} {
		p := windowPace(c.w, now)
		if p.marker != c.marker || p.limit != c.limit {
			t.Errorf("%s: marker=%v limit=%v, want %v %v", c.name, p.marker, p.limit, c.marker, c.limit)
			continue
		}
		if c.marker && (p.expected-c.expected > 1e-6 || c.expected-p.expected > 1e-6) {
			t.Errorf("%s: expected %v, want %v", c.name, p.expected, c.expected)
		}
		if c.limit && p.hit != c.hit {
			t.Errorf("%s: hit in %v, want %v", c.name, p.hit, c.hit)
		}
		if got := tone(c.w, p); got != c.tone {
			t.Errorf("%s: tone %q, want %q", c.name, got, c.tone)
		}
	}
}
