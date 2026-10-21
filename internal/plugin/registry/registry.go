// Package registry names the plugins the app knows: the ones built into
// the app, and the ones a user installed under its plugin folder.
package registry

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"kero/internal/plugin/protocol"
)

// A built-in plugin is a role of the app's own binary, as kerod is: its
// manifest sits in the binary, and the app starts it with
//
//	kero plugin-run <id>
//
// The manifests here are embedded, so a built-in plugin adds a JSON file
// and the code that serves it, not a path to find at run time.
//
//go:embed manifests
var manifests embed.FS

// Plugin is a plugin the app knows: its manifest, where it comes from, and
// how to serve it when it is built in. Dir is the plugin's folder when it
// was installed, empty when it is built in.
type Plugin struct {
	Manifest protocol.Manifest
	Builtin  bool
	Dir      string
	Serve    func() error
}

// The code of the built-in plugins, registered by their packages' init.
// A plugin's manifest can be in the app before its code is: the registry
// is what the settings page lists, and the runner is what plugin-run
// serves.
var (
	mu         sync.Mutex
	runners    = map[string]func() error{}
	builtins   []Plugin
	builtinErr error
	builtinOne sync.Once
)

// RegisterRunner attaches the code of a built-in plugin to the manifest of
// its id. A built-in plugin's package calls this from init.
func RegisterRunner(id string, serve func() error) {
	mu.Lock()
	defer mu.Unlock()
	if id == "" || serve == nil {
		panic("registry: a built-in plugin needs an id and a serve function")
	}
	if _, ok := runners[id]; ok {
		panic(fmt.Sprintf("registry: plugin %q registered twice", id))
	}
	runners[id] = serve
}

// loadBuiltins reads the embedded manifests once, with the runners that
// exist by the first call: every plugin package's init has run by then.
func loadBuiltins() {
	builtinOne.Do(func() {
		names, err := fs.Glob(manifests, "manifests/*.json")
		if err != nil {
			builtinErr = err
			return
		}
		for _, name := range names {
			b, err := manifests.ReadFile(name)
			if err != nil {
				builtinErr = err
				return
			}
			var m protocol.Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				builtinErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			if err := m.Validate(); err != nil {
				builtinErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			mu.Lock()
			serve := runners[m.ID]
			mu.Unlock()
			builtins = append(builtins, Plugin{Manifest: m, Builtin: true, Serve: serve})
		}
		sort.Slice(builtins, func(i, j int) bool { return builtins[i].Manifest.Name < builtins[j].Manifest.Name })
	})
}

// Builtins returns the plugins built into the app, by name.
func Builtins() ([]Plugin, error) {
	loadBuiltins()
	if builtinErr != nil {
		return nil, builtinErr
	}
	return append([]Plugin(nil), builtins...), nil
}

// Installed returns the plugins under dir, a folder per plugin with its
// kero-plugin.json: proto.Dir()/plugins. A folder that does not read, or
// whose manifest is not valid, is not a plugin and is left out. A missing
// folder is no plugin installed, not an error: the app makes it when a
// user installs the first one.
func Installed(dir string) []Plugin {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var plugins []Plugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folder := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(filepath.Join(folder, protocol.ManifestFile))
		if err != nil {
			continue
		}
		var m protocol.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		if err := m.Validate(); err != nil {
			continue
		}
		// The folder names the plugin, as plugin-run does.
		if m.ID != e.Name() {
			continue
		}
		plugins = append(plugins, Plugin{Manifest: m, Dir: folder})
	}
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Manifest.Name < plugins[j].Manifest.Name
	})
	return plugins
}

// Known returns the plugins the app knows: its own first, then the ones a
// user installed under dir. An installed plugin never shadows a built-in
// one.
func Known(dir string) []Plugin {
	builtins, _ := Builtins()
	known := append([]Plugin(nil), builtins...)
	seen := map[string]bool{}
	for _, p := range builtins {
		seen[p.Manifest.ID] = true
	}
	for _, p := range Installed(dir) {
		if seen[p.Manifest.ID] {
			continue
		}
		known = append(known, p)
	}
	return known
}

// Find returns the plugin with the id.
func Find(plugins []Plugin, id string) (Plugin, bool) {
	for _, p := range plugins {
		if p.Manifest.ID == id {
			return p, true
		}
	}
	return Plugin{}, false
}

// Argv is how a plugin starts: the app's own executable for a built-in
// one, its manifest's command for an installed one.
func Argv(p Plugin) ([]string, error) {
	if p.Builtin {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return []string{exe, "plugin-run", p.Manifest.ID}, nil
	}
	if len(p.Manifest.Run) == 0 {
		return nil, fmt.Errorf("plugin %q has no command to run", p.Manifest.ID)
	}
	argv := append([]string(nil), p.Manifest.Run...)
	if !filepath.IsAbs(argv[0]) {
		argv[0] = filepath.Join(p.Dir, argv[0])
	}
	return argv, nil
}

// Serve runs a built-in plugin: what "kero plugin-run <id>" does.
func Serve(id string) error {
	builtins, err := Builtins()
	if err != nil {
		return err
	}
	p, ok := Find(builtins, id)
	if !ok {
		return fmt.Errorf("no built-in plugin %q", id)
	}
	if p.Serve == nil {
		return fmt.Errorf("the %s plugin has no code in this build", p.Manifest.Name)
	}
	return p.Serve()
}
