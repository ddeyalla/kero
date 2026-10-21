package protocol

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ManifestFile is the file that declares a plugin, in its folder.
const ManifestFile = "kero-plugin.json"

// The permissions a manifest may ask for.
const (
	PermSecrets = "secrets" // the plugin's own secrets, kept by the app
	PermNotify  = "notify"  // a notification the user sees
	PermOpenURL = "openURL" // open a link in the browser
)

// KnownPermissions are the permissions a manifest may ask for.
var KnownPermissions = []string{PermSecrets, PermNotify, PermOpenURL}

// The kinds of setting a manifest may declare. v1 has one.
const SettingText = "text"

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	settingRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// Manifest is kero-plugin.json: what a plugin is, what it gives, and
// what it asks Shibu for.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	// Run starts the plugin: an argv. A plugin of the app's is the
	// app's own executable with the plugin's role.
	Run         []string    `json:"run"`
	Widgets     []Widget    `json:"widgets"`
	Commands    []Command   `json:"commands,omitempty"`
	Permissions Permissions `json:"permissions,omitempty"`
	Settings    []Setting   `json:"settings,omitempty"`
}

// Permissions are what a plugin asks Shibu for. A permission controls
// only what Shibu gives, not what the process may read on disk.
type Permissions []string

// Has tells whether the plugin asked for a permission.
func (p Permissions) Has(permission string) bool { return slices.Contains(p, permission) }

// Widget is one pane a plugin gives.
type Widget struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	MinWidth  int    `json:"minWidth,omitempty"`
	MinHeight int    `json:"minHeight,omitempty"`
	// Compact is the widget's shape in a plugin slot (BRU-165), which
	// no plugin has yet: the least size the slot shows it at.
	Compact *WidgetSize `json:"compact,omitempty"`
}

// WidgetSize is a widget's least size.
type WidgetSize struct {
	MinWidth  int `json:"minWidth,omitempty"`
	MinHeight int `json:"minHeight,omitempty"`
}

// Command is one of the plugin's palette commands, which is also a
// `kero do` action.
type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Setting is one field of the plugin's settings, drawn in Settings ›
// Plugins. A secret value goes to the plugin's secrets instead, and the
// host never sends it back.
type Setting struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Secret bool   `json:"secret,omitempty"`
	Help   string `json:"help,omitempty"`
	Link   string `json:"link,omitempty"`
}

// WidgetByName returns the widget with the name, nil none.
func (m *Manifest) WidgetByName(name string) *Widget {
	for i := range m.Widgets {
		if m.Widgets[i].Name == name {
			return &m.Widgets[i]
		}
	}
	return nil
}

// Validate checks a manifest before the app shows or runs it.
func (m *Manifest) Validate() error {
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("plugin manifest: id %q must match %s", m.ID, idRe)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("plugin manifest %s: a name is required", m.ID)
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin manifest %s: a version is required", m.ID)
	}
	if len(m.Run) == 0 || strings.TrimSpace(m.Run[0]) == "" {
		return fmt.Errorf("plugin manifest %s: run is required", m.ID)
	}
	if len(m.Widgets) == 0 {
		return fmt.Errorf("plugin manifest %s: a widget is required", m.ID)
	}
	widgets := map[string]bool{}
	for _, w := range m.Widgets {
		if !nameRe.MatchString(w.Name) {
			return fmt.Errorf("plugin manifest %s: widget name %q must match %s", m.ID, w.Name, nameRe)
		}
		if widgets[w.Name] {
			return fmt.Errorf("plugin manifest %s: widget %q is repeated", m.ID, w.Name)
		}
		widgets[w.Name] = true
		if strings.TrimSpace(w.Title) == "" {
			return fmt.Errorf("plugin manifest %s: widget %q needs a title", m.ID, w.Name)
		}
		if w.MinWidth < 0 || w.MinHeight < 0 {
			return fmt.Errorf("plugin manifest %s: widget %q has a negative least size", m.ID, w.Name)
		}
	}
	commands := map[string]bool{}
	for _, c := range m.Commands {
		if !nameRe.MatchString(c.ID) {
			return fmt.Errorf("plugin manifest %s: command id %q must match %s", m.ID, c.ID, nameRe)
		}
		if commands[c.ID] {
			return fmt.Errorf("plugin manifest %s: command %q is repeated", m.ID, c.ID)
		}
		commands[c.ID] = true
		if strings.TrimSpace(c.Title) == "" {
			return fmt.Errorf("plugin manifest %s: command %q needs a title", m.ID, c.ID)
		}
	}
	for _, p := range m.Permissions {
		if !slices.Contains(KnownPermissions, p) {
			return fmt.Errorf("plugin manifest %s: permission %q is not one of %s", m.ID, p, strings.Join(KnownPermissions, ", "))
		}
	}
	settings := map[string]bool{}
	for _, s := range m.Settings {
		if !settingRe.MatchString(s.Key) {
			return fmt.Errorf("plugin manifest %s: setting key %q must match %s", m.ID, s.Key, settingRe)
		}
		if settings[s.Key] {
			return fmt.Errorf("plugin manifest %s: setting %q is repeated", m.ID, s.Key)
		}
		settings[s.Key] = true
		if strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("plugin manifest %s: setting %q needs a label", m.ID, s.Key)
		}
		if s.Kind != SettingText {
			return fmt.Errorf("plugin manifest %s: setting %q has kind %q, not %q", m.ID, s.Key, s.Kind, SettingText)
		}
	}
	return nil
}
