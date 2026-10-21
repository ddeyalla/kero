// Package settings is kero's settings: one file, settings.json in kero's
// directory, that the app writes from its Settings window and kerod reads
// as it starts panes, so that the shell, its environment and the
// scrollback apply to new panes at once.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"kero/internal/proto"
)

// Settings are the user's choices. The zero value of a field is never a
// choice of its own: Load fills defaults in.
type Settings struct {
	// General
	NewSessionDir   string `json:"newSessionDir"`   // "current", "project" or "home"
	SessionName     string `json:"sessionName"`     // what names an unnamed session: proto.NameFocused, NameLead or NameFolder
	ConfirmClose    bool   `json:"confirmClose"`    // before closing a pane a program runs in
	RestoreSessions bool   `json:"restoreSessions"` // after kerod restarts, as after a reboot
	ResumeAgents    bool   `json:"resumeAgents"`    // as it restores sessions: the agents' own
	QuitConfirm     bool   `json:"quitConfirm"`     // tell, the first time, that quitting keeps sessions

	// Appearance
	Appearance string `json:"appearance"` // "system", "light" or "dark"
	Language   string `json:"language"`   // "system", or one of Languages
	Accent     string `json:"accent"`     // an accent preset, see Accents: its color, wash and terminal selection
	Background string `json:"background"` // a wash, see Backgrounds
	Vibrancy   bool   `json:"vibrancy"`   // the window's material under the wash
	Grain      bool   `json:"grain"`      // a faint grain over the wash

	// Terminal
	FontFamily   string  `json:"fontFamily"` // "" the font kero carries; "monospace" the system's
	FontSize     float32 `json:"fontSize"`
	LineHeight   float32 `json:"lineHeight"`
	FontWeight   int     `json:"fontWeight"`
	Ligatures    bool    `json:"ligatures"`
	Thicken      bool    `json:"thicken"`
	MinContrast  float32 `json:"minContrast"` // a WCAG ratio text keeps with its background
	Cursor       string  `json:"cursor"`      // "block", "bar" or "underline"
	CursorBlink  bool    `json:"cursorBlink"`
	LightTheme   string  `json:"lightTheme"` // "kero", or a Ghostty theme's name
	DarkTheme    string  `json:"darkTheme"`
	OptionAsAlt  bool    `json:"optionAsAlt"`
	CopyOnSelect bool    `json:"copyOnSelect"`
	Clipboard    string  `json:"clipboard"`  // programs reading it: "ask", "allow" or "deny"
	RightClick   string  `json:"rightClick"` // "menu", "paste" or "copyOrPaste"

	// Shell
	Shell      string   `json:"shell"` // "" the account's login shell
	Env        []string `json:"env"`   // "KEY=value", added to every pane's
	Scrollback int      `json:"scrollback"`

	// Notifications
	NotifyWaiting bool `json:"notifyWaiting"` // a session needs you
	NotifyDone    bool `json:"notifyDone"`    // an agent finished
	NotifyFailed  bool `json:"notifyFailed"`  // a program reported it failed
	NotifySound   bool `json:"notifySound"`
	// NotifyThrottle is how many minutes the same session and state stays
	// quiet after it told: 0 tells every transition.
	NotifyThrottle int `json:"notifyThrottle"`
	// SilencedSessions are the sessions whose notifications the user
	// turned off, by their id.
	SilencedSessions []string `json:"silencedSessions,omitempty"`
	DockBadge        bool     `json:"dockBadge"`
	BounceDock       bool     `json:"bounceDock"`

	// Agents
	RemoteControl bool `json:"remoteControl"` // kero focus and kero do drive the app
	AgentHooks    bool `json:"agentHooks"`    // agents' hooks report their turns (kero hook)

	// Linear
	LinearSidebar      bool   `json:"linearSidebar"`      // the workspace's row in the sidebar
	LinearRefresh      int    `json:"linearRefresh"`      // seconds between reads of what changed, at least LinearMinRefresh
	LinearEmptyColumns string `json:"linearEmptyColumns"` // states with no issues: "collapsed", "shown" or "hidden"
	LinearDensity      string `json:"linearDensity"`      // the board's cards: "comfortable" or "compact"

	// Keyboard: accelerators by action, over the defaults; "" unbinds.
	Shortcuts map[string]string `json:"shortcuts,omitempty"`
}

// Default returns the settings of a new install.
func Default() Settings {
	return Settings{
		NewSessionDir:   "current",
		SessionName:     proto.NameFocused,
		ConfirmClose:    true,
		RestoreSessions: true,
		ResumeAgents:    true,
		QuitConfirm:     true,
		Appearance:      "system",
		Language:        "system",
		Accent:          AccentDefault,
		Background:      "rex",
		Vibrancy:        true,
		Grain:           true,
		FontSize:        13,
		LineHeight:      1.22,
		FontWeight:      400,
		Ligatures:       true,
		MinContrast:     3,
		Cursor:          "block",
		CursorBlink:     true,
		LightTheme:      "kero",
		DarkTheme:       "kero",
		Clipboard:       "ask",
		RightClick:      "menu",
		Scrollback:      4,
		NotifyWaiting:   true,
		NotifyDone:      true,
		NotifyFailed:    true,
		NotifyThrottle:  5,
		DockBadge:       true,
		RemoteControl:   true,
		AgentHooks:      true,

		LinearSidebar:      true,
		LinearRefresh:      30,
		LinearEmptyColumns: "collapsed",
		LinearDensity:      "comfortable",
	}
}

// LinearMinRefresh is the shortest wait between two reads of the Linear
// workspace, in seconds: Linear allows a key 2 500 requests an hour.
const LinearMinRefresh = 15

// Languages are the languages the app is written in, as the catalogs of
// kero/locales name them.
var Languages = []string{"en", "zh-Hans"}

// Backgrounds are the washes the window can show.
var Backgrounds = []struct{ ID, Name string }{
	{"rex", "Petal"},
	{"mist", "Mist"},
	{"meadow", "Meadow"},
	{"dusk", "Dusk"},
	{"plum", "Plum"},
	{"rose", "Rose"},
	{"sea", "Sea"},
	{"plain", "Plain"},
}

// Accents are the accent presets: an accent's color in the light and the
// dark, the wash that comes with it, and the terminals' selection with it.
var Accents = []struct{ ID, Name, Wash string }{
	{"blue", "Blue", "rex"},
	{"plum", "Plum", "plum"},
	{"rose", "Rose", "rose"},
	{"sea", "Sea", "sea"},
	{"moss", "Moss", "meadow"},
	{"graphite", "Graphite", "mist"},
}

// AccentDefault is the accent of a new install: the blue the app has had.
const AccentDefault = "blue"

// Path returns the settings' file, the configuration (ConfigPath).
func Path() string { return ConfigPath() }

// legacyPath is where kero kept its settings before the configuration
// file, as JSON, which the first Load moves into it.
func legacyPath() string { return filepath.Join(proto.Dir(), "settings.json") }

var (
	mu       sync.Mutex
	cache    *Settings
	mtime    int64
	problems []string
)

// Load returns the settings, the defaults where the file says nothing.
// It reads the file again when it changed, so that a long-running reader,
// as kerod, sees the app's changes, and the user's.
func Load() Settings {
	mu.Lock()
	defer mu.Unlock()
	migrate()
	st, err := os.Stat(Path())
	if err != nil {
		cache, problems = nil, nil
		return Default()
	}
	if cache != nil && st.ModTime().UnixNano() == mtime {
		return clone(*cache)
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		return Default()
	}
	s, probs := parse(string(b))
	cache, mtime, problems = &s, st.ModTime().UnixNano(), probs
	return clone(s)
}

// Problems returns what the file got wrong, as of the last Load: keys
// kero does not know, and values it cannot take, which keep their
// defaults.
func Problems() []string {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(problems)
}

// migrate moves the settings of the JSON file kero kept before into the
// configuration, once; mu is held.
func migrate() {
	if _, err := os.Stat(Path()); err == nil {
		return
	}
	b, err := os.ReadFile(legacyPath())
	if err != nil {
		return
	}
	s := Default()
	if json.Unmarshal(b, &s) != nil {
		return
	}
	s.normalize()
	if write(s) == nil {
		os.Rename(legacyPath(), legacyPath()+".migrated")
	}
}

// Save writes the settings into the configuration, keeping what the user
// wrote around them (see format).
func Save(s Settings) error {
	mu.Lock()
	defer mu.Unlock()
	s.normalize()
	if err := write(s); err != nil {
		return err
	}
	c := clone(s)
	cache, problems = &c, nil
	if st, err := os.Stat(Path()); err == nil {
		mtime = st.ModTime().UnixNano()
	}
	return nil
}

// write writes the configuration; mu is held.
func write(s Settings) error {
	old, _ := os.ReadFile(Path())
	data := format(s, string(old))
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// Clone returns a copy that shares nothing with s.
func (s Settings) Clone() Settings { return clone(s) }

func clone(s Settings) Settings {
	s.Env = append([]string(nil), s.Env...)
	s.SilencedSessions = append([]string(nil), s.SilencedSessions...)
	if s.Shortcuts != nil {
		m := make(map[string]string, len(s.Shortcuts))
		for k, v := range s.Shortcuts {
			m[k] = v
		}
		s.Shortcuts = m
	}
	return s
}

// normalize keeps values in range, as a file edited by hand may not.
func (s *Settings) normalize() {
	d := Default()
	oneOf := func(v *string, def string, ok ...string) {
		for _, o := range ok {
			if *v == o {
				return
			}
		}
		*v = def
	}
	oneOf(&s.NewSessionDir, d.NewSessionDir, "current", "project", "home")
	oneOf(&s.Appearance, d.Appearance, "system", "light", "dark")
	oneOf(&s.Language, d.Language, append([]string{"system"}, Languages...)...)
	known := false
	for _, a := range Accents {
		known = known || a.ID == s.Accent
	}
	if !known {
		s.Accent = d.Accent
	}
	known = false
	for _, b := range Backgrounds {
		known = known || b.ID == s.Background
	}
	if !known {
		s.Background = d.Background
	}
	oneOf(&s.Cursor, d.Cursor, "block", "bar", "underline")
	oneOf(&s.Clipboard, d.Clipboard, "ask", "allow", "deny")
	oneOf(&s.RightClick, d.RightClick, "menu", "paste", "copyOrPaste")
	if s.FontSize < 8 || s.FontSize > 36 {
		s.FontSize = d.FontSize
	}
	if s.LineHeight < 0.8 || s.LineHeight > 2.5 {
		s.LineHeight = d.LineHeight
	}
	if s.FontWeight < 100 || s.FontWeight > 900 {
		s.FontWeight = d.FontWeight
	}
	if s.Scrollback < 0 || s.Scrollback > 256 {
		s.Scrollback = d.Scrollback
	}
	if s.NotifyThrottle < 0 || s.NotifyThrottle > 60 {
		s.NotifyThrottle = d.NotifyThrottle
	}
	s.SilencedSessions = cleanSessions(s.SilencedSessions)
	if s.LightTheme == "" {
		s.LightTheme = d.LightTheme
	}
	if s.DarkTheme == "" {
		s.DarkTheme = d.DarkTheme
	}
}

// cleanSessions keeps a list of session ids as the one form of them: each
// once, with none empty, as the settings file and the app's toggle may say
// it.
func cleanSessions(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
