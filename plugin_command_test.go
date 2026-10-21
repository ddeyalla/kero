package main

import (
	"testing"

	"kero/internal/plugin/host"
	"kero/locales"
)

// A palette command for a Spotify pane whose plugin process is gone says so
// instead of dropping the host's error.
func TestSpotifyCommandReportsAFailedHostCommand(t *testing.T) {
	a := &App{}
	a.plugins.panes = map[string]*pluginPaneState{"a": {plugin: "spotify"}}
	a.plugins.host = host.New(host.Config{Dir: t.TempDir()})

	a.spotifyCommand("next")

	if a.pendingToast != locales.PluginCommandFailed() {
		t.Fatalf("toast = %q, want %q", a.pendingToast, locales.PluginCommandFailed())
	}
}
