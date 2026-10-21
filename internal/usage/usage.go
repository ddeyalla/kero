// Package usage reads account allowances, independently of terminal sessions.
// Credentials stay in memory and are sent only to their provider's HTTPS host.
package usage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
)

type Window struct {
	Label  string    `json:"label"`
	Used   float64   `json:"usedPercent"`
	Resets time.Time `json:"resetsAt"`
}

type Report struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	URL     string    `json:"url"`
	Plan    string    `json:"plan,omitempty"`
	Balance string    `json:"balance,omitempty"`
	Windows []Window  `json:"windows"`
	Checked time.Time `json:"checkedAt"`
	Error   string    `json:"error,omitempty"`
	RetryAt time.Time `json:"retryAt,omitzero"`
}

func Providers() []Report {
	return []Report{
		{ID: "claude", Name: "Claude", URL: "https://claude.ai/settings/usage"},
		{ID: "codex", Name: "Codex", URL: "https://chatgpt.com/codex/settings/usage"},
		{ID: "opencode", Name: "OpenCode Go", URL: "https://opencode.ai/auth"},
		{ID: "deepseek", Name: "DeepSeek", URL: "https://platform.deepseek.com/usage"},
	}
}

// Refresh retains the last successful observation on failure, and obeys a
// provider's backoff even when the user requests a manual refresh.
func Refresh(ctx context.Context, previous []Report) []Report {
	out := Providers()
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			old := out[i]
			for _, p := range previous {
				if p.ID == old.ID {
					old = p
					break
				}
			}
			if time.Now().Before(old.RetryAt) {
				out[i] = old
				return
			}
			r, err := fetch(ctx, out[i].ID)
			if err != nil {
				old.Error = err.Error()
				old.RetryAt = r.RetryAt
				out[i] = old
				return
			}
			r.ID, r.Name, r.URL, r.Checked = out[i].ID, out[i].Name, out[i].URL, time.Now()
			out[i] = r
		}()
	}
	wg.Wait()
	return out
}

func configDir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

func readJSON(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(out)
}

type login struct {
	Type      string `json:"type"`
	Key       string `json:"key"`
	Access    string `json:"access"`
	AccountID string `json:"accountId"`
}

func openCodeLogins() map[string]login {
	var auth map[string]login
	readJSON(filepath.Join(configDir("XDG_DATA_HOME", ".local/share"), "opencode", "auth.json"), &auth)
	return auth
}

type claudeAuth struct {
	OAuth struct {
		Token string `json:"accessToken"`
		Plan  string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// claudeFile reads Claude Code's sign-in file. Installed uses it rather than
// the keychain: looking for what is set up should not ask the system for a
// secret.
func claudeFile() (token, plan string) {
	var auth claudeAuth
	readJSON(filepath.Join(configDir("CLAUDE_CONFIG_DIR", ".claude"), ".credentials.json"), &auth)
	return auth.OAuth.Token, auth.OAuth.Plan
}

func claudeCredentials(ctx context.Context) (token, plan string) {
	var auth claudeAuth
	if runtime.GOOS == "darwin" {
		service := "Claude Code-credentials"
		if profile := os.Getenv("CLAUDE_CONFIG_DIR"); profile != "" {
			hash := sha256.Sum256([]byte(norm.NFC.String(profile)))
			service += fmt.Sprintf("-%x", hash[:4])
		}
		if u, err := user.Current(); err == nil {
			cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service, "-a", u.Username, "-w")
			if b, err := cmd.Output(); err == nil {
				json.Unmarshal(b, &auth)
			}
		}
	}
	if auth.OAuth.Token == "" {
		readJSON(filepath.Join(configDir("CLAUDE_CONFIG_DIR", ".claude"), ".credentials.json"), &auth)
	}
	return auth.OAuth.Token, auth.OAuth.Plan
}

// codexFile reads Codex's sign-in file, without OpenCode's login fallback.
func codexFile() (token, account string) {
	var auth struct {
		Tokens struct {
			Access  string `json:"access_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	readJSON(filepath.Join(configDir("CODEX_HOME", ".codex"), "auth.json"), &auth)
	return auth.Tokens.Access, auth.Tokens.Account
}

func codexCredentials() (token, account string) {
	if token, account := codexFile(); token != "" {
		return token, account
	}
	if a := openCodeLogins()["openai"]; a.Type == "oauth" {
		return a.Access, a.AccountID
	}
	return "", ""
}

func openCodeKey() string {
	if key := savedKey("opencode"); key != "" {
		return key
	}
	if key := os.Getenv("OPENCODE_API_KEY"); key != "" {
		return key
	}
	auth := openCodeLogins()
	for _, name := range []string{"opencode-go", "opencode"} {
		if a := auth[name]; a.Key != "" {
			return a.Key
		}
	}
	return ""
}

func deepSeekKey() string {
	if key := savedKey("deepseek"); key != "" {
		return key
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		return key
	}
	return openCodeLogins()["deepseek"].Key
}

func credentials(ctx context.Context, id string) (token, account, plan string) {
	switch id {
	case "claude":
		token, plan = claudeCredentials(ctx)
		return token, "", plan
	case "codex":
		token, account = codexCredentials()
		return token, account, ""
	case "opencode":
		return openCodeKey(), "", ""
	case "deepseek":
		return deepSeekKey(), "", ""
	}
	return "", "", ""
}

// installDirs are where the harnesses' installers put their commands, for
// an app opened from the Dock or the Finder, whose PATH is only the
// system's (/usr/bin:/bin:/usr/sbin:/sbin): Claude Code's installer links
// ~/.local/bin/claude (~/.claude/local before it), OpenCode's
// ~/.opencode/bin, Bun's global installs ~/.bun/bin, and Homebrew and npm
// /opt/homebrew/bin or /usr/local/bin. "~/" is the home folder.
var installDirs = []string{"~/.local/bin", "~/.claude/local", "~/.opencode/bin", "~/.bun/bin", "/opt/homebrew/bin", "/usr/local/bin"}

// command reports whether this Mac has the command name: on PATH, or an
// executable file in one of installDirs.
func command(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	for _, dir := range installDirs {
		if rest, ok := strings.CutPrefix(dir, "~/"); ok {
			if home == "" {
				continue
			}
			dir = filepath.Join(home, rest)
		}
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}

// Installed returns the providers this Mac has set up: those whose CLI is on
// PATH or in an installer's folder (installDirs), whose sign-in files exist,
// or, for DeepSeek, whose key is found. The sidebar shows these, so a new
// user, with none, sees every provider it can connect instead of an empty
// row. Claude Code keeps its sign-in in the keychain, which this does not
// ask, so its command is what finds it.
func Installed() []string {
	var ids []string
	if command("claude") {
		ids = append(ids, "claude")
	} else if token, _ := claudeFile(); token != "" {
		ids = append(ids, "claude")
	}
	if command("codex") {
		ids = append(ids, "codex")
	} else if token, _ := codexFile(); token != "" {
		ids = append(ids, "codex")
	}
	if command("opencode") || openCodeKey() != "" {
		ids = append(ids, "opencode")
	}
	if deepSeekKey() != "" {
		ids = append(ids, "deepseek")
	}
	if len(ids) == 0 {
		for _, r := range Providers() {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

var endpoints = map[string]string{
	"claude":   "https://api.anthropic.com/api/oauth/usage",
	"codex":    "https://chatgpt.com/backend-api/wham/usage",
	"opencode": "https://opencode.ai/zen/go/v1/usage",
	"deepseek": "https://api.deepseek.com/user/balance",
}

func fetch(ctx context.Context, id string) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	token, account, plan := credentials(ctx, id)
	if token == "" {
		return Report{}, errors.New("Not connected")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints[id], nil)
	if err != nil {
		return Report{}, errors.New("Unable to request usage")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Kero-Local/1")
	if id == "claude" {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		req.Header.Set("User-Agent", "claude-code/2.1.0")
	}
	if id == "codex" {
		req.Header.Set("User-Agent", "codex-cli")
		if account != "" {
			req.Header.Set("ChatGPT-Account-Id", account)
		}
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return Report{}, errors.New("Could not reach provider")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		next := time.Now().Add(10 * time.Minute)
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
			next = time.Now().Add(time.Duration(min(seconds, 86400)) * time.Second)
		} else if at, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && at.After(next) {
			next = at
		}
		return Report{RetryAt: next}, errors.New("Provider rate limited · retry later")
	}
	if resp.StatusCode == 401 {
		return Report{}, errors.New("Sign in again")
	}
	if resp.StatusCode == 403 {
		return Report{}, errors.New("Account access unavailable")
	}
	if resp.StatusCode != http.StatusOK {
		return Report{}, fmt.Errorf("Provider unavailable (HTTP %d)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return Report{}, errors.New("Could not read provider response")
	}
	r, err := parse(id, body, time.Now())
	if r.Plan == "" {
		r.Plan = plan
	}
	return r, err
}

var errFormat = errors.New("Usage format unavailable")

func parse(id string, body []byte, now time.Time) (Report, error) {
	r := Report{}
	add := func(label string, percent *float64, reset time.Time) {
		if percent != nil && !math.IsNaN(*percent) && !math.IsInf(*percent, 0) && *percent >= 0 && *percent <= 100 {
			r.Windows = append(r.Windows, Window{label, *percent, reset})
		}
	}
	switch id {
	case "claude":
		type window struct {
			Used  *float64 `json:"utilization"`
			Reset string   `json:"resets_at"`
		}
		var v struct {
			Five   *window `json:"five_hour"`
			Week   *window `json:"seven_day"`
			Limits []struct {
				Kind    string   `json:"kind"`
				Percent *float64 `json:"percent"`
				Reset   string   `json:"resets_at"`
			} `json:"limits"`
		}
		if json.Unmarshal(body, &v) != nil {
			return r, errFormat
		}
		for _, w := range v.Limits {
			label := map[string]string{"session": "5 hours", "weekly_all": "Weekly"}[w.Kind]
			if label != "" {
				at, _ := time.Parse(time.RFC3339, w.Reset)
				add(label, w.Percent, at)
			}
		}
		if len(r.Windows) == 0 {
			for _, w := range []struct {
				label string
				value *window
			}{{"5 hours", v.Five}, {"Weekly", v.Week}} {
				if w.value != nil {
					at, _ := time.Parse(time.RFC3339, w.value.Reset)
					add(w.label, w.value.Used, at)
				}
			}
		}
	case "codex":
		type window struct {
			Used    *float64 `json:"used_percent"`
			Seconds int      `json:"limit_window_seconds"`
			Reset   int64    `json:"reset_at"`
		}
		var v struct {
			Plan  string `json:"plan_type"`
			Limit struct {
				Primary   *window `json:"primary_window"`
				Secondary *window `json:"secondary_window"`
			} `json:"rate_limit"`
		}
		if json.Unmarshal(body, &v) != nil {
			return r, errFormat
		}
		r.Plan = v.Plan
		for _, w := range []*window{v.Limit.Primary, v.Limit.Secondary} {
			if w == nil {
				continue
			}
			label := "Allowance"
			if w.Seconds == 604800 {
				label = "Weekly"
			} else if w.Seconds > 0 {
				label = fmt.Sprintf("%g hours", float64(w.Seconds)/3600)
			}
			var at time.Time
			if w.Reset > 0 {
				at = time.Unix(w.Reset, 0)
			}
			add(label, w.Used, at)
		}
	case "opencode":
		var v struct {
			Usage map[string]struct {
				Percent      *float64        `json:"percent"`
				UsagePercent *float64        `json:"usagePercent"`
				Used         *float64        `json:"used"`
				Limit        float64         `json:"limit"`
				ResetIn      *float64        `json:"resetInSec"`
				ResetAt      json.RawMessage `json:"resetAt"`
				ResetsAt     json.RawMessage `json:"resetsAt"`
			} `json:"usage"`
		}
		if json.Unmarshal(body, &v) != nil {
			return r, errFormat
		}
		for _, name := range []string{"rolling", "weekly", "monthly"} {
			w, ok := v.Usage[name]
			if !ok {
				continue
			}
			percent := w.Percent
			if percent == nil {
				percent = w.UsagePercent
			}
			if percent == nil && w.Used != nil && w.Limit > 0 {
				p := *w.Used / w.Limit * 100
				percent = &p
			}
			var at time.Time
			if w.ResetIn != nil && *w.ResetIn >= 0 && *w.ResetIn < 366*86400 {
				at = now.Add(time.Duration(*w.ResetIn * float64(time.Second)))
			}
			reset := w.ResetsAt
			if len(reset) == 0 || string(reset) == "null" {
				reset = w.ResetAt
			}
			if at.IsZero() && len(reset) > 0 {
				var s string
				var n int64
				if json.Unmarshal(reset, &s) == nil {
					at, _ = time.Parse(time.RFC3339, s)
				} else if json.Unmarshal(reset, &n) == nil && n > 0 {
					if n > 1e12 {
						at = time.UnixMilli(n)
					} else {
						at = time.Unix(n, 0)
					}
				}
			}
			add(map[string]string{"rolling": "5 hours", "weekly": "Weekly", "monthly": "Monthly"}[name], percent, at)
		}
	case "deepseek":
		var v struct {
			Balances []struct {
				Currency string `json:"currency"`
				Total    string `json:"total_balance"`
			} `json:"balance_infos"`
		}
		if json.Unmarshal(body, &v) != nil {
			return r, errFormat
		}
		var parts []string
		for _, b := range v.Balances {
			n, err := strconv.ParseFloat(b.Total, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (b.Currency != "USD" && b.Currency != "CNY") {
				return r, errFormat
			}
			parts = append(parts, b.Currency+" "+b.Total)
		}
		if len(parts) == 0 {
			return r, errFormat
		}
		r.Balance = strings.Join(parts, " · ")
		r.Plan = "API credit"
		return r, nil
	}
	if len(r.Windows) == 0 {
		return r, errFormat
	}
	return r, nil
}
