package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kero/internal/actions"
)

func TestFormatKeepsTheUsersFile(t *testing.T) {
	file := `# my kero
font-size = 14

# colors
theme = light:Catppuccin Latte,dark:Catppuccin Mocha
some-future-key = yes
env = A=1
env = B=2
`
	s, probs := parse(file)
	if s.FontSize != 14 || s.LightTheme != "Catppuccin Latte" || s.DarkTheme != "Catppuccin Mocha" {
		t.Fatalf("parsed %+v", s)
	}
	if !reflect.DeepEqual(s.Env, []string{"A=1", "B=2"}) {
		t.Errorf("env %q", s.Env)
	}
	if len(probs) != 1 || !strings.Contains(probs[0], "some-future-key") {
		t.Errorf("problems %q", probs)
	}
	s.FontSize = 15
	s.Env = []string{"C=3"}
	s.CursorBlink = false
	out := format(s, file)
	want := `# my kero
font-size = 15

# colors
theme = light:Catppuccin Latte,dark:Catppuccin Mocha
some-future-key = yes
env = C=3
cursor-style-blink = false
`
	if out != want {
		t.Errorf("formatted:\n%s\nwant:\n%s", out, want)
	}
	back, _ := parse(out)
	if !reflect.DeepEqual(back, s) {
		t.Errorf("read back %+v, want %+v", back, s)
	}
}

func TestFormatNewFileHasOnlyChanges(t *testing.T) {
	s := Default()
	settingLines := func(out string) int {
		n := 0
		for _, l := range strings.Split(out, "\n") {
			if l != "" && !strings.HasPrefix(l, "#") {
				n++
			}
		}
		return n
	}
	if out := format(s, ""); settingLines(out) != 0 {
		t.Errorf("defaults wrote settings:\n%s", out)
	}
	s.FontFamily, s.Appearance = "Iosevka Term", "dark"
	out := format(s, "")
	if !strings.Contains(out, "window-theme = dark\n") || !strings.Contains(out, "font-family = Iosevka Term\n") {
		t.Errorf("formatted:\n%s", out)
	}
	if n := settingLines(out); n != 2 {
		t.Errorf("%d settings written, want 2:\n%s", n, out)
	}
}

func TestKeybinds(t *testing.T) {
	s, probs := parse("keybind = cmd+shift+x=pane.split.right\nkeybind = cmd+d=unbind\nkeybind = cmd+==text.bigger\nkeybind = ctrl+q=nope\n")
	if s.Shortcuts["pane.split.right"] != "CmdOrCtrl+Shift+X" {
		t.Errorf("split right %q", s.Shortcuts["pane.split.right"])
	}
	if v, ok := s.Shortcuts["pane.split.right"]; !ok || v == "" {
		t.Errorf("split right lost to the unbind: %q", v)
	}
	if s.Shortcuts["text.bigger"] != "CmdOrCtrl+=" {
		t.Errorf("bigger %q", s.Shortcuts["text.bigger"])
	}
	if len(probs) != 1 || !strings.Contains(probs[0], "nope") {
		t.Errorf("problems %q", probs)
	}
	// Unbinding keys frees the action that has them.
	split, _ := actions.Find("pane.split.right")
	keys := configKeys(split.Keys) // cmd+d on macOS
	s2, _ := parse("keybind = " + keys + "=unbind\n")
	if v, ok := s2.Shortcuts["pane.split.right"]; !ok || v != "" {
		t.Errorf("%s unbound, split right %q %v", keys, v, ok)
	}
	if out := format(s2, ""); !strings.Contains(out, "keybind = "+keys+"=unbind\n") {
		t.Errorf("formatted:\n%s", out)
	}
}

// The accent's preset and the grain read back, and a preset the file
// does not have keeps the default.
func TestAccentAndGrain(t *testing.T) {
	s, probs := parse("accent = plum\ngrain = false\n")
	if s.Accent != "plum" || s.Grain {
		t.Errorf("parsed %+v", s)
	}
	if len(probs) != 0 {
		t.Errorf("problems %q", probs)
	}
	s, probs = parse("accent = gold\n")
	if s.Accent != AccentDefault {
		t.Errorf("gold kept as %q", s.Accent)
	}
	if len(probs) != 1 {
		t.Errorf("problems %q", probs)
	}
	// Written only when they differ from the defaults.
	if out := format(s, ""); strings.Contains(out, "accent") || strings.Contains(out, "grain") {
		t.Errorf("defaults wrote:\n%s", out)
	}
	s2 := Default()
	s2.Accent, s2.Grain = "sea", false
	if out := format(s2, ""); !strings.Contains(out, "accent = sea\n") || !strings.Contains(out, "grain = false\n") {
		t.Errorf("formatted:\n%s", out)
	}
}

func TestBadValuesKeepDefaults(t *testing.T) {
	s, probs := parse("cursor-style = triangle\nfont-size = big\nconfirm-close = maybe\n")
	d := Default()
	if s.Cursor != d.Cursor || s.FontSize != d.FontSize || s.ConfirmClose != d.ConfirmClose {
		t.Errorf("bad values changed settings: %+v", s)
	}
	if len(probs) != 3 {
		t.Errorf("problems %q", probs)
	}
}

func TestMigratesSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KERO_HOME", dir)
	t.Setenv("KERO_CONFIG", filepath.Join(dir, "config", "kero", "config"))
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"fontSize": 16, "appearance": "dark", "background": "rex"}`), 0o600)
	mu.Lock()
	cache = nil
	mu.Unlock()
	s := Load()
	if s.FontSize != 16 || s.Appearance != "dark" {
		t.Fatalf("migrated %+v", s)
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "font-size = 16") || strings.Contains(string(b), "background") {
		t.Errorf("config:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Error("settings.json stayed")
	}
}

// The Linear keys read back, a refresh shorter than Linear's quota allows
// is raised to the floor, and a bad choice keeps the default.
func TestLinearKeys(t *testing.T) {
	s, probs := parse("linear-sidebar = false\nlinear-refresh-interval = 60\nlinear-empty-columns = shown\nlinear-card-density = compact\n")
	if len(probs) != 0 {
		t.Fatalf("problems %q", probs)
	}
	if s.LinearSidebar || s.LinearRefresh != 60 || s.LinearEmptyColumns != "shown" || s.LinearDensity != "compact" {
		t.Fatalf("parsed %+v", s)
	}
	back, _ := parse(format(s, ""))
	if !reflect.DeepEqual(back, s) {
		t.Errorf("read back %+v, want %+v", back, s)
	}

	s, _ = parse("linear-refresh-interval = 2\n")
	if s.LinearRefresh != LinearMinRefresh {
		t.Errorf("refresh %d, want the floor %d", s.LinearRefresh, LinearMinRefresh)
	}
	s, _ = parse("linear-refresh-interval = 99999\n")
	if s.LinearRefresh != 300 {
		t.Errorf("refresh %d, want the ceiling 300", s.LinearRefresh)
	}

	d := Default()
	s, probs = parse("linear-empty-columns = sideways\nlinear-card-density = tiny\nlinear-sidebar = sometimes\n")
	if s.LinearEmptyColumns != d.LinearEmptyColumns || s.LinearDensity != d.LinearDensity || s.LinearSidebar != d.LinearSidebar {
		t.Errorf("bad values changed settings: %+v", s)
	}
	if len(probs) != 3 {
		t.Errorf("problems %q", probs)
	}
}

// The notifications' new keys: the wait between two tellings of the same
// session and state, in minutes, and the sessions the user silenced, each
// once, which read back as they were written.
func TestNotifyThrottleAndSilence(t *testing.T) {
	s, probs := parse("notify-throttle-minutes = 2\nsilence-session = s1\nsilence-session = s2\nsilence-session = s1\n")
	if len(probs) != 0 {
		t.Fatalf("problems %q", probs)
	}
	if s.NotifyThrottle != 2 {
		t.Errorf("throttle %d", s.NotifyThrottle)
	}
	if !reflect.DeepEqual(s.SilencedSessions, []string{"s1", "s2"}) {
		t.Errorf("silenced %q", s.SilencedSessions)
	}
	out := format(s, "")
	if !strings.Contains(out, "notify-throttle-minutes = 2\n") || !strings.Contains(out, "silence-session = s1\n") {
		t.Errorf("formatted:\n%s", out)
	}
	// A wait outside 0 to 60 is taken to its end.
	s, _ = parse("notify-throttle-minutes = 500\n")
	if s.NotifyThrottle != 60 {
		t.Errorf("throttle %d, want 60", s.NotifyThrottle)
	}
}
