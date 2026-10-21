package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// kero do knows the commands of the plugins the app knows, as
// plugin.<plugin>.<command>, and no other.
func TestDoKnowsAPluginsCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KERO_HOME", home)
	dir := filepath.Join(home, "plugins", "example")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"example","name":"Example","version":"1.0.0","run":["example"],
		"widgets":[{"name":"main","title":"Example"}],
		"commands":[{"id":"refresh","title":"Refresh Example"}]}`
	if err := os.WriteFile(filepath.Join(dir, "kero-plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if !pluginCommand("plugin.example.refresh") {
		t.Fatal("the installed plugin's command is not an action")
	}
	for _, id := range []string{"plugin.example.nope", "plugin.other.refresh", "plugin.example", "pane.new"} {
		if pluginCommand(id) {
			t.Errorf("%q is a plugin's command", id)
		}
	}
}
