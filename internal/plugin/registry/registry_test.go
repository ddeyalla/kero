package registry

import (
	"os"
	"path/filepath"
	"testing"
)

// writePlugin writes an installed plugin's folder, with its manifest.
func writePlugin(t *testing.T, dir, folder, manifest string) string {
	t.Helper()
	path := filepath.Join(dir, folder)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "kero-plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestInstalledPluginsAreReadFromTheirFolders covers an installed plugin:
// its manifest, and the command its folder holds.
func TestInstalledPluginsAreReadFromTheirFolders(t *testing.T) {
	dir := t.TempDir()
	folder := writePlugin(t, dir, "demo", `{
		"id": "demo",
		"name": "Demo",
		"version": "0.1.0",
		"description": "A plugin in a test.",
		"run": ["bin/demo"],
		"widgets": [{"name": "main", "title": "Main"}],
		"permissions": ["notify"]
	}`)
	plugins := Installed(dir)
	if len(plugins) != 1 {
		t.Fatalf("Installed read %d plugins, want 1", len(plugins))
	}
	p := plugins[0]
	if p.Builtin {
		t.Error("an installed plugin is not built in")
	}
	if p.Dir != folder {
		t.Errorf("Dir is %q, want %q", p.Dir, folder)
	}
	if p.Manifest.Name != "Demo" || p.Manifest.Version != "0.1.0" {
		t.Errorf("manifest is %+v", p.Manifest)
	}
	argv, err := Argv(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(folder, "bin/demo"); len(argv) != 1 || argv[0] != want {
		t.Errorf("Argv is %q, want %q", argv, want)
	}
}

// TestAnInstalledManifestMustNameItsFolder keeps a stray folder from
// passing for another plugin.
func TestAnInstalledManifestMustNameItsFolder(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "one", `{
		"id": "two",
		"name": "Two",
		"version": "0.1.0",
		"run": ["demo"],
		"widgets": [{"name": "main", "title": "Main"}]
	}`)
	plugins := Installed(dir)
	if len(plugins) != 0 {
		t.Fatalf("Installed read %d plugins, want none: %+v", len(plugins), plugins)
	}
}

// TestKnownListsInstalledPlugins covers the list the app shows.
func TestKnownListsInstalledPlugins(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "demo", `{
		"id": "demo",
		"name": "Demo",
		"version": "0.1.0",
		"run": ["demo"],
		"widgets": [{"name": "main", "title": "Main"}]
	}`)
	plugins := Known(dir)
	if _, ok := Find(plugins, "demo"); !ok {
		t.Fatalf("Known is %+v, want the demo plugin", plugins)
	}
	if _, ok := Find(plugins, "spotify"); !ok {
		t.Fatalf("Known is %+v, want the built-in spotify plugin", plugins)
	}
	if _, ok := Find(plugins, "demo"); !ok {
		t.Error("the demo plugin is not found")
	}
	if _, ok := Find(plugins, "other"); ok {
		t.Error("a plugin that is not there is found")
	}
}
