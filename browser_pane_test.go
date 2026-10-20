package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

func TestBrowserURL(t *testing.T) {
	for typed, want := range map[string]string{
		"":                        "",
		"3000":                    "http://localhost:3000",
		"localhost:5173":          "http://localhost:5173",
		"localhost":               "http://localhost",
		"127.0.0.1:8080/api":      "http://127.0.0.1:8080/api",
		"[::1]:3000":              "http://[::1]:3000",
		"app.localhost:3000":      "http://app.localhost:3000",
		"192.168.1.20:8000":       "http://192.168.1.20:8000",
		"example.com":             "https://example.com",
		"example.com/docs?q=1":    "https://example.com/docs?q=1",
		"https://linear.app":      "https://linear.app",
		"http://localhost:3000/x": "http://localhost:3000/x",
		"go generics":             "https://duckduckgo.com/?q=go+generics",
		"kero":                    "https://duckduckgo.com/?q=kero",
		"file:///etc/passwd":      "",
		"javascript:alert(1)":     "",
		"ftp://example.com/file":  "",
	} {
		if got := browserURL(typed); got != want {
			t.Errorf("browserURL(%q) = %q, want %q", typed, got, want)
		}
	}
	for raw, want := range map[string]bool{
		"http://localhost:3000": true, "https://example.com": true, "about:blank": true,
		"file:///etc/passwd": false, "kero://x": false, "about:config": false, "data:text/html,hi": false,
	} {
		if webOnly(raw) != want {
			t.Errorf("webOnly(%q) = %v", raw, !want)
		}
	}
}

func TestParseListeners(t *testing.T) {
	out := []byte("p501\ncnode\nn127.0.0.1:5173\nn[::1]:5173\np502\ncControlCe\nn*:7000\nn*:5000\np503\ncpython3.1\nn*:8000\np504\ncpostgres\nn127.0.0.1:5432\np505\ncsshd\nn*:22\n")
	got := parseListeners(out)
	want := []devServer{{5173, "node"}, {5432, "postgres"}, {8000, "python3.1"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %v, want %v", i, got[i], want[i])
		}
	}
}

// testBrowser is the session of testApp with pane b a Browser pane that
// shows no page yet.
func testBrowser(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	b := &a.st.Sessions[0].Panes[1]
	b.View, b.Shell = proto.PaneBrowser, ""
	return a
}

// Before a page, the pane offers the servers running here, and its
// address; one picked is the page, which is the pane's Target.
func TestBrowserStartPage(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testBrowser(t)
		st := a.browserStateOf(&a.st.Sessions[0].Panes[1])
		st.servers, st.serverAt = []devServer{{3000, "node"}, {5173, "node"}, {8000, "python3"}}, time.Now()
		tt := ui.NewTester(a.view, 1280, 720)
		tt.SetDark(dark)
		frames(tt)
		for _, want := range []string{"Browser", "Running on this Mac", "localhost:3000", "localhost:5173", "python3", "Back", "Reload"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		shot(t, tt, map[bool]string{false: "browser-start-light", true: "browser-start-dark"}[dark])
	}
	a := testBrowser(t)
	st := a.browserStateOf(&a.st.Sessions[0].Panes[1])
	st.servers, st.serverAt = []devServer{{5173, "node"}}, time.Now()
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	tt.Click("localhost:5173")
	frames(tt)
	if st.url != "http://localhost:5173" || st.address != "http://localhost:5173" {
		t.Fatalf("picking a server showed %q", st.url)
	}
	if st.sent != "http://localhost:5173" {
		t.Fatalf("the page did not become the Target: sent %q", st.sent)
	}
}

// A Target as typed opens as the web page it means, never the app's.
func TestBrowserTargetIsMadeAURL(t *testing.T) {
	a := testBrowser(t)
	b := &a.st.Sessions[0].Panes[1]
	b.Target = "3000"
	if st := a.browserStateOf(b); st.url != "http://localhost:3000" {
		t.Fatalf("Target 3000 showed %q", st.url)
	}
	a = testBrowser(t)
	b = &a.st.Sessions[0].Panes[1]
	b.Target = "/settings"
	if st := a.browserStateOf(b); st.url == "/settings" {
		t.Fatal("a path showed as a page of the app")
	}
}

// What a closed pane's view held goes with it.
func TestSweepViews(t *testing.T) {
	a := testBrowser(t)
	a.browserStateOf(&a.st.Sessions[0].Panes[1])
	a.tasksState("b")
	a.files = map[string]*filesState{"b": {}, "gone": {}}
	a.browsers["gone"] = &browserState{}
	a.sweepViews()
	if a.browsers["gone"] != nil || a.files["gone"] != nil || a.browsers["b"] == nil || a.files["b"] == nil || a.tasks["b"] == nil {
		t.Fatal("swept the wrong views")
	}
	a.st.Sessions[0].Panes = a.st.Sessions[0].Panes[:1]
	a.sweepViews()
	if len(a.browsers)+len(a.files)+len(a.tasks) != 0 {
		t.Fatal("a closed pane's view stayed")
	}
}
