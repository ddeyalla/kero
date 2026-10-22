package protocol

import "encoding/json"

// Size is a pane's size in points, as the host tells the plugin.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// HelloParams is the host's greeting: the protocol it speaks, and its
// name and version, for a plugin that shows them.
type HelloParams struct {
	Protocol int    `json:"protocol"`
	Host     string `json:"host,omitempty"`
}

// HelloResult is the plugin's answer: the protocol it speaks.
type HelloResult struct {
	Protocol int `json:"protocol"`
}

// OpenParams asks a plugin to draw one of its panes.
type OpenParams struct {
	Pane   string          `json:"pane"`
	Widget string          `json:"widget"`
	State  json.RawMessage `json:"state,omitempty"`
	Size   Size            `json:"size"`
}

// CloseParams tells a plugin that a pane closed.
type CloseParams struct {
	Pane string `json:"pane"`
}

// ResizeParams tells a plugin that a pane changed size.
type ResizeParams struct {
	Pane string `json:"pane"`
	Size Size   `json:"size"`
}

// VisibleParams tells a plugin whether a pane is shown, so that it
// polls or animates only while the user sees it.
type VisibleParams struct {
	Pane    string `json:"pane"`
	Visible bool   `json:"visible"`
}

// ThemeParams is the appearance the app draws with.
type ThemeParams struct {
	Dark   bool   `json:"dark"`
	Accent string `json:"accent,omitempty"` // a hex color, as "#7f5af0"
}

// SettingsParams are the plugin's settings, without the secret ones.
type SettingsParams struct {
	Values map[string]string `json:"values"`
}

// EventParams is what the user did with a part, by the part's id.
type EventParams struct {
	Pane  string `json:"pane"`
	Node  string `json:"node"`
	Kind  string `json:"kind"`
	Value any    `json:"value,omitempty"`
}

// CommandParams runs one of the plugin's commands, from the palette or
// `kero do`.
type CommandParams struct {
	ID string `json:"id"`
}

// RenderParams replaces what a pane draws.
type RenderParams struct {
	Pane string `json:"pane"`
	Tree Tree   `json:"tree"`
}

// StateSaveParams saves a pane's state in its target, so that it comes
// back after a restart.
type StateSaveParams struct {
	Pane  string          `json:"pane"`
	State json.RawMessage `json:"state"`
}

// SecretGetParams reads one of the plugin's own secrets.
type SecretGetParams struct {
	Key string `json:"key"`
}

// SecretGetResult is the secret's value, "" when it is not set.
type SecretGetResult struct {
	Value string `json:"value"`
}

// SecretSetParams keeps one of the plugin's own secrets. An empty value
// removes it.
type SecretSetParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// NotifyParams is a notification the user sees.
type NotifyParams struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// OpenURLParams opens a link in the browser.
type OpenURLParams struct {
	URL string `json:"url"`
}

// WebOpenParams runs a page of this machine in a web view with no window:
// the plugin's own engine, such as one that plays audio. The page is
// http on a loopback address, and the plugin talks to it by that server.
// The page of an id that is open already is loaded again.
type WebOpenParams struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// WebCloseParams ends the web view of an id. The host also ends every
// web view of a plugin whose process stops.
type WebCloseParams struct {
	ID string `json:"id"`
}
