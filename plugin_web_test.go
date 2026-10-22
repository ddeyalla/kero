package main

import (
	"errors"
	"testing"
)

// fakeWebs records the web views the app made and what became of them.
type fakeWebs struct {
	made, loaded, destroyed []string
}

func (f *fakeWebs) newWeb(url string) (*pluginWeb, error) {
	f.made = append(f.made, url)
	return &pluginWeb{
		load:    func(url string) { f.loaded = append(f.loaded, url) },
		destroy: func() { f.destroyed = append(f.destroyed, url) },
	}, nil
}

func TestPluginWebViewRunsLoadsAgainAndEnds(t *testing.T) {
	f := &fakeWebs{}
	a := &App{}
	a.plugins.newWeb = f.newWeb

	a.pluginWebOpen("spotify", "engine", "http://127.0.0.1:9/a")
	a.pluginWebOpen("spotify", "engine", "http://127.0.0.1:9/b")
	if len(f.made) != 1 || len(f.loaded) != 1 || f.loaded[0] != "http://127.0.0.1:9/b" {
		t.Fatalf("made %v and loaded %v, want one view that loads the second page", f.made, f.loaded)
	}
	// Another plugin's id is another view.
	a.pluginWebOpen("other", "engine", "http://127.0.0.1:9/c")
	if len(f.made) != 2 {
		t.Fatalf("made %v, want a view for each plugin", f.made)
	}

	a.pluginWebClose("spotify", "engine")
	a.pluginWebClose("spotify", "engine")
	if len(f.destroyed) != 1 || f.destroyed[0] != "http://127.0.0.1:9/a" {
		t.Fatalf("destroyed %v, want the first view once", f.destroyed)
	}
	if a.plugins.webs["other/engine"] == nil || a.plugins.webs["spotify/engine"] != nil {
		t.Fatalf("webs = %v, want only the other plugin's", a.plugins.webs)
	}
}

func TestPluginWebViewThatFailsToStartLeavesNone(t *testing.T) {
	a := &App{}
	a.plugins.newWeb = func(string) (*pluginWeb, error) { return nil, errors.New("no web view") }
	a.pluginWebOpen("spotify", "engine", "http://127.0.0.1:9/a")
	if len(a.plugins.webs) != 0 || a.plugins.notice != "no web view" {
		t.Fatalf("webs = %v, notice = %q", a.plugins.webs, a.plugins.notice)
	}
}
