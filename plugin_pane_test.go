package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/proto"
	"kero/locales"
)

func TestPluginPaneComesBackFromItsTarget(t *testing.T) {
	a := &App{}
	target, err := protocol.EncodeTarget(protocol.Target{
		Plugin: "fake",
		Widget: "main",
		State:  json.RawMessage(`{"track":"one"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &proto.Pane{ID: "p1", View: proto.PanePlugins, Target: target}
	st := a.pluginPaneState(p)
	if st.plugin != "fake" || st.widget != "main" {
		t.Fatalf("pane = %q/%q", st.plugin, st.widget)
	}
	if string(st.state) != `{"track":"one"}` {
		t.Fatalf("state = %s", st.state)
	}
	if st.view == nil {
		t.Fatal("the pane has no view")
	}

	// A state the plugin saved reaches the target through the daemon, and
	// the next frame reads it back.
	target, err = protocol.EncodeTarget(protocol.Target{
		Plugin: "fake",
		Widget: "main",
		State:  json.RawMessage(`{"track":"two"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.Target = target
	st = a.pluginPaneState(p)
	if string(st.state) != `{"track":"two"}` {
		t.Fatalf("state after the target changed = %s", st.state)
	}
}

func TestPluginPaneWithABadTargetIsEmpty(t *testing.T) {
	a := &App{}
	st := a.pluginPaneState(&proto.Pane{ID: "p1", View: proto.PanePlugins, Target: "not json"})
	if st.plugin != "" || st.widget != "" {
		t.Fatalf("a bad target read as %q/%q", st.plugin, st.widget)
	}
	if st.view == nil {
		t.Fatal("the pane has no view")
	}
}

func TestOldDaemonSaysRestartTheService(t *testing.T) {
	err := errors.New(`pane.replace: no view "plugin"`)
	got := viewCallError(err)
	if got != locales.PluginNeedsRestart() {
		t.Fatalf("message = %q", got)
	}
	if strings.Contains(got, "no view") {
		t.Fatalf("the raw daemon error reached the user: %q", got)
	}
	// Any other error is the user's to read as it is.
	other := errors.New("the daemon is not running")
	if got := viewCallError(other); got != other.Error() {
		t.Fatalf("message = %q", got)
	}
}

func TestPluginWidgetsComeFromTheRegistry(t *testing.T) {
	a := &App{}
	a.plugins.known = []registry.Plugin{{
		Manifest: protocol.Manifest{
			ID:      "fake",
			Name:    "Fake",
			Version: "1.0.0",
			Run:     []string{"fake"},
			Widgets: []protocol.Widget{
				{Name: "main", Title: "Main"},
				{Name: "other", Title: "Other"},
			},
		},
		Builtin: true,
	}}
	widgets := a.pluginWidgets()
	if len(widgets) != 2 {
		t.Fatalf("widgets = %d", len(widgets))
	}
	if widgets[0].plugin != "fake" || widgets[0].widget != "main" || widgets[0].title != "Fake" || widgets[0].detail != "Main" {
		t.Fatalf("first widget = %+v", widgets[0])
	}
	kinds := a.chooserKinds()
	if len(kinds) != len(paneKinds)+2 {
		t.Fatalf("chooser kinds = %d, want %d", len(kinds), len(paneKinds)+2)
	}
	if kinds[len(paneKinds)].plugin != "fake" {
		t.Fatalf("the first plugin row is %+v", kinds[len(paneKinds)])
	}
}
