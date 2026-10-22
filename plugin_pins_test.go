package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/proto"
	"kero/locales"
)

// pinApp is a test app that knows one plugin, Fake Board, with a widget
// that has an icon, and shows the session s1 of testApp.
func pinApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.applyPlugins([]registry.Plugin{{Manifest: protocol.Manifest{
		ID: "fake", Name: "Fake Board", Version: "1", Run: []string{"fake"},
		Widgets: []protocol.Widget{{Name: "board", Title: "Board", Icon: "board"}},
	}, Builtin: true}})
	// The pins the first run seeds are the migration's test.
	a.prefs.Pinned = nil
	return a
}

// withPluginPane adds a pane of the fake plugin's board to a session.
func withPluginPane(t *testing.T, a *App, session, pane string) {
	t.Helper()
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: "fake", Widget: "board"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.st.Sessions {
		if a.st.Sessions[i].ID == session {
			s := &a.st.Sessions[i]
			s.Panes = append(s.Panes, proto.Pane{ID: pane, View: proto.PanePlugins, Target: target, Progress: -1})
			if s.Layout != nil {
				s.Layout = s.Layout.Split(s.Panes[0].ID, pane, "right", 0)
			}
		}
	}
}

// A pinned plugin has a row in the sidebar, with the plugin's name; a pin
// of a plugin that is not installed has none, and is kept.
func TestPinnedPluginShowsInTheSidebar(t *testing.T) {
	a := pinApp(t)
	a.prefs.Pinned = []pinRef{{"gone", "board"}}
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	if tt.HasText("Fake Board") {
		t.Fatalf("a row for a plugin that is not pinned; texts %q", tt.Texts())
	}
	a.prefs.Pinned = append(a.prefs.Pinned, pinRef{"fake", "board"})
	tt.Frame()
	if !tt.HasText("Fake Board") {
		t.Fatalf("no row for the pinned plugin; texts %q", tt.Texts())
	}
	if len(a.prefs.Pinned) != 2 {
		t.Fatalf("pins = %v, want the pin of the missing plugin kept", a.prefs.Pinned)
	}
	shot(t, tt, "pin-row")
}

// A click on a pin focuses the plugin's pane in the selected session; with
// none there, even if another session has one, it splits one in.
func TestPinClickFocusesOrSplits(t *testing.T) {
	a := pinApp(t)
	a.st.Sessions = append(a.st.Sessions, proto.Session{ID: "s2", Project: "p1", Name: "other",
		Panes: []proto.Pane{{ID: "z", Shell: "zsh", Dir: "/tmp", Progress: -1}}, Focus: "z"})
	withPluginPane(t, a, "s2", "other-board")
	a.prefs.Pinned = []pinRef{{"fake", "board"}}
	type split struct{ session, plugin, widget string }
	var splits []split
	a.plugins.openPane = func(s *proto.Session, plugin, widget string) {
		splits = append(splits, split{s.ID, plugin, widget})
	}
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()

	if err := tt.Click("Fake Board"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !slices.Equal(splits, []split{{"s1", "fake", "board"}}) {
		t.Fatalf("splits = %v, want one pane split into the selected session, not the other's pane focused", splits)
	}
	if a.selected != "s1" {
		t.Fatalf("selected = %q, want the pin to stay in s1", a.selected)
	}

	// The session now has the pane: a click focuses it and splits nothing.
	withPluginPane(t, a, "s1", "board")
	splits = nil
	if err := tt.Click("Fake Board"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(splits) != 0 {
		t.Fatalf("splits = %v, want the existing pane focused", splits)
	}
	if got := a.focusOf(a.session("s1")); got != "board" {
		t.Fatalf("focus = %q, want the plugin's pane", got)
	}
}

// Pinning and unpinning go to the prefs file, and come back after a
// restart; a plugin that is not installed, or has no such widget, is not
// pinned.
func TestPinUnpinRoundTrip(t *testing.T) {
	a := pinApp(t)
	if a.pinPlugin("gone", "") || a.pinPlugin("fake", "nope") {
		t.Fatal("pinned a plugin or widget that is not there")
	}
	if len(a.prefs.Pinned) != 0 {
		t.Fatalf("pins = %v", a.prefs.Pinned)
	}
	if !a.pinPlugin("fake", "") {
		t.Fatal("did not pin the first widget")
	}
	a.pinPlugin("fake", "board")
	if !slices.Equal(a.prefs.Pinned, []pinRef{{"fake", "board"}}) {
		t.Fatalf("pins = %v, want the first widget pinned once", a.prefs.Pinned)
	}
	// The prefs are written in the background: a restart sees them once
	// they are flushed, as quitting does.
	a.flushPrefs()
	if again := newApp(); !slices.Contains(again.prefs.Pinned, pinRef{"fake", "board"}) {
		t.Fatalf("pins after a restart = %v", again.prefs.Pinned)
	}
	a.unpinPlugin("fake", "")
	if len(a.prefs.Pinned) != 0 {
		t.Fatalf("pins = %v after an unpin", a.prefs.Pinned)
	}
	a.flushPrefs()
	if again := newApp(); slices.Contains(again.prefs.Pinned, pinRef{"fake", "board"}) {
		t.Fatalf("an unpinned plugin came back: %v", again.prefs.Pinned)
	}
	// An unpin of a plugin that is gone still works.
	a.prefs.Pinned = []pinRef{{"gone", "board"}, {"fake", "board"}}
	a.unpinPlugin("gone", "")
	if !slices.Equal(a.prefs.Pinned, []pinRef{{"fake", "board"}}) {
		t.Fatalf("pins = %v, want only the pin of the missing plugin gone", a.prefs.Pinned)
	}
}

// The Linear row of the sidebar becomes the pin of the Linear plugin's
// board, once: unpinned later, it does not come back.
func TestLinearSidebarSeedsAPinOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KERO_HOME", home)
	t.Setenv("KERO_CONFIG", filepath.Join(home, "config"))
	a := newApp()
	if !a.set.LinearSidebar {
		t.Fatal("the Linear row is not on by default")
	}
	if !slices.Equal(a.prefs.Pinned, []pinRef{{"linear", "board"}}) || !a.prefs.PinsSeeded {
		t.Fatalf("pins = %v seeded %v, want the Linear board", a.prefs.Pinned, a.prefs.PinsSeeded)
	}
	a.savePrefs()
	a.unpinPlugin("linear", "board")
	a.flushPrefs()
	b := newApp()
	if len(b.prefs.Pinned) != 0 {
		t.Fatalf("pins = %v, want the migration not to run twice", b.prefs.Pinned)
	}

	// With the row off, there is nothing to pin, and it is still done.
	c := &App{}
	c.set.LinearSidebar = false
	c.migratePins()
	if len(c.prefs.Pinned) != 0 || !c.prefs.PinsSeeded {
		t.Fatalf("pins = %v seeded %v, want none, seeded", c.prefs.Pinned, c.prefs.PinsSeeded)
	}
}

// The palette offers to pin and unpin, and the actions of `kero plugin`
// do the same.
func TestPinPaletteAndCommand(t *testing.T) {
	a := pinApp(t)
	find := func(title string) *paletteItem {
		for _, it := range a.paletteItems() {
			if it.title == title {
				return &it
			}
		}
		return nil
	}
	pin := find(locales.PalettePinPlugin("Fake Board"))
	if pin == nil {
		t.Fatal("the palette has no command to pin the plugin")
	}
	pin.run()
	if !a.pinned("fake", "board") {
		t.Fatal("the palette command did not pin")
	}
	if find(locales.PalettePinPlugin("Fake Board")) != nil {
		t.Fatal("the palette still offers to pin a pinned plugin")
	}
	unpin := find(locales.PaletteUnpinPlugin("Fake Board"))
	if unpin == nil {
		t.Fatal("the palette has no command to unpin the plugin")
	}
	unpin.run()
	if a.pinned("fake", "board") {
		t.Fatal("the palette command did not unpin")
	}
	a.run("plugin.pin", map[string]string{"plugin": "fake"})
	if !a.pinned("fake", "board") {
		t.Fatal("plugin.pin did not pin")
	}
	a.run("plugin.unpin", map[string]string{"plugin": "fake", "widget": "board"})
	if a.pinned("fake", "board") {
		t.Fatal("plugin.unpin did not unpin")
	}
}

// A plugin pane's header menu offers Pin to Sidebar, and Unpin from it
// once pinned.
func TestPluginPaneMenuPins(t *testing.T) {
	a := pinApp(t)
	withPluginPane(t, a, "s1", "board")
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	if err := tt.RightClick(paneViewName(proto.PanePlugins)); err != nil {
		t.Fatalf("no header for the plugin pane: %v; texts %q", err, tt.Texts())
	}
	if menu := tt.Menu(); !slices.Contains(menu, locales.PluginPin()) {
		t.Fatalf("menu = %q, want Pin to Sidebar", menu)
	}
	if err := tt.ChooseMenuItem(locales.PluginPin()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.pinned("fake", "board") {
		t.Fatal("the menu did not pin")
	}
	if err := tt.RightClick(paneViewName(proto.PanePlugins)); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, locales.PluginUnpin()) {
		t.Fatalf("menu = %q, want Unpin from Sidebar", menu)
	}
}

// Settings › Plugins has a toggle for each widget.
func TestPluginSettingsPinToggle(t *testing.T) {
	a := pinApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	a.openSettings("plugins")
	tt.Frame()
	if !tt.HasText(locales.PluginPinSetting()) {
		t.Fatalf("no pin toggle; texts %q", tt.Texts())
	}
}
