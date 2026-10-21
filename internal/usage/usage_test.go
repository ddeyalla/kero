package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestFetchFailuresAndBackoff(t *testing.T) {
	t.Setenv("KERO_HOME", t.TempDir())
	t.Setenv("OPENCODE_API_KEY", "test-key")
	old := endpoints["opencode"]
	t.Cleanup(func() { endpoints["opencode"] = old })
	for _, tc := range []struct {
		status     int
		body, want string
	}{
		{401, `{"secret":"must not appear"}`, "Sign in again"},
		{403, `{}`, "Account access unavailable"},
		{429, `{}`, "Provider rate limited · retry later"},
		{200, `{}`, "Usage format unavailable"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing authorization")
				}
				w.Header().Set("Retry-After", "600")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			endpoints["opencode"] = server.URL
			r, err := fetch(context.Background(), "opencode")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error: %v", err)
			}
			if tc.status == 429 && time.Until(r.RetryAt) < 9*time.Minute {
				t.Fatal("missing backoff")
			}
		})
	}
}

func TestRefreshKeepsObservationDuringBackoff(t *testing.T) {
	previous := Providers()
	for i := range previous {
		previous[i].Checked = time.Now().Add(-time.Minute)
		previous[i].RetryAt = time.Now().Add(time.Hour)
		previous[i].Windows = []Window{{Label: "Weekly", Used: 42}}
		previous[i].Error = "Provider rate limited · retry later"
	}
	for _, r := range Refresh(context.Background(), previous) {
		if len(r.Windows) != 1 || r.Windows[0].Used != 42 || r.Error == "" {
			t.Fatalf("lost stale observation: %+v", r)
		}
	}
}

func TestSavedKeysStayPrivate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KERO_HOME", dir)
	if err := SaveKey("deepseek", "test-key"); err != nil {
		t.Fatal(err)
	}
	if key := savedKey("deepseek"); key != "test-key" {
		t.Fatal("saved key unavailable")
	}
	st, err := os.Stat(filepath.Join(dir, "usage-credentials.json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("credentials not private: %v, %v", st, err)
	}
	if err := SaveKey("claude", "test-key"); err == nil {
		t.Fatal("accepted unsupported credential override")
	}
	if err := SaveKey("deepseek", "key\nheader"); err == nil {
		t.Fatal("accepted newline in key")
	}
	if err := SaveKey("deepseek", ""); err != nil {
		t.Fatal(err)
	}
	if savedKey("deepseek") != "" {
		t.Fatal("key not removed")
	}
}

func TestProviderWindows(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		provider string
		body     string
		want     int
		label    string
		used     float64
	}{
		{"claude", `{"five_hour":{"utilization":23.5,"resets_at":"2027-01-15T08:00:00Z"},"seven_day":{"utilization":0}}`, 2, "5 hours", 23.5},
		{"claude", `{"limits":[{"kind":"session","percent":35,"resets_at":"2027-01-15T08:00:00Z"},{"kind":"weekly_all","percent":12}]}`, 2, "5 hours", 35},
		{"codex", `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":604800,"reset_at":1800000900},"secondary_window":null}}`, 1, "Weekly", 20},
		{"opencode", `{"usage":{"rolling":{"usagePercent":0.5,"resetInSec":600},"weekly":{"used":25,"limit":100,"resetInSec":900}}}`, 2, "5 hours", 0.5},
		{"opencode", `{"usage":{"rolling":{"percent":12.5,"resetsAt":"2027-01-15T08:00:00Z","status":"ok"},"weekly":{"percent":25,"resetsAt":"2027-01-18T08:00:00Z","status":"ok"},"monthly":{"percent":50,"resetsAt":"2027-02-01T08:00:00Z","status":"ok"}}}`, 3, "5 hours", 12.5},
	} {
		t.Run(tc.provider+tc.label+tc.body[:12], func(t *testing.T) {
			r, err := parse(tc.provider, []byte(tc.body), now)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Windows) != tc.want || r.Windows[0].Label != tc.label || r.Windows[0].Used != tc.used {
				t.Fatalf("unexpected windows: %+v", r.Windows)
			}
		})
	}
}

func TestInvalidUsageIsNotZero(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"error":"no"}`, `{"rate_limit":{"primary_window":{"used_percent":-1}}}`, `{"rate_limit":{"primary_window":{"used_percent":101}}}`} {
		if _, err := parse("codex", []byte(body), time.Now()); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestDeepSeekBalance(t *testing.T) {
	r, err := parse("deepseek", []byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"20.00"},{"currency":"USD","total_balance":"4.32"}]}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Balance != "CNY 20.00 · USD 4.32" {
		t.Fatalf("balance %q", r.Balance)
	}
	if _, err := parse("deepseek", []byte(`{"balance_infos":[{"currency":"USD","total_balance":"oops"}]}`), time.Now()); err == nil {
		t.Fatal("accepted invalid balance")
	}
}

// Installed lists the harnesses this Mac has set up, from their command or
// their sign-in, and every provider when it has none.
func TestInstalled(t *testing.T) {
	home := t.TempDir()
	for _, env := range []string{"HOME", "XDG_DATA_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "KERO_HOME"} {
		t.Setenv(env, filepath.Join(home, env))
	}
	t.Setenv("PATH", filepath.Join(home, "bin"))
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	all := []string{"claude", "codex", "opencode", "deepseek"}
	if got := Installed(); !slices.Equal(got, all) {
		t.Fatalf("nothing set up: %v", got)
	}

	codex := filepath.Join(home, "CODEX_HOME")
	if err := os.MkdirAll(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "auth.json"), []byte(`{"tokens":{"access_token":"test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Installed(); !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("a sign-in file was missed: %v", got)
	}

	if err := SaveKey("deepseek", "test-key"); err != nil {
		t.Fatal(err)
	}
	if got := Installed(); !slices.Equal(got, []string{"codex", "deepseek"}) {
		t.Fatalf("a saved key was missed: %v", got)
	}

	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Installed(); !slices.Equal(got, []string{"claude", "codex", "deepseek"}) {
		t.Fatalf("a command on PATH was missed: %v", got)
	}
}
