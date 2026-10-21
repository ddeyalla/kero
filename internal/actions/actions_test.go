package actions

import "testing"

// A plugin's command is an action as plugin.<plugin>.<command>, and splits
// back one way only.
func TestPluginCommandIDs(t *testing.T) {
	id := PluginCommand("spotify", "next-track")
	if id != "plugin.spotify.next-track" {
		t.Fatalf("id = %q", id)
	}
	plugin, command, ok := ParsePluginCommand(id)
	if !ok || plugin != "spotify" || command != "next-track" {
		t.Fatalf("parsed %q as %q, %q, %v", id, plugin, command, ok)
	}
	for _, bad := range []string{"pane.new", "plugin.", "plugin.spotify", "plugin..next", "plugin.spotify.", "plugin.a.b.c"} {
		if _, _, ok := ParsePluginCommand(bad); ok {
			t.Errorf("%q parsed as a plugin's command", bad)
		}
	}
	if _, ok := Find(id); ok {
		t.Fatal("a plugin's command is in All")
	}
}
