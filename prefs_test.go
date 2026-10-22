package main

import (
	"encoding/json"
	"os"
	"testing"

	"kero/internal/settings"
)

// readPrefs is app.json as the disk has it.
func readPrefs(t *testing.T) prefs {
	t.Helper()
	b, err := os.ReadFile(prefsPath())
	if err != nil {
		t.Fatal(err)
	}
	var p prefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Changes that come as a burst, as a session switch and a sidebar drag make
// them, cost one write, and the file holds the last of them.
func TestPrefsWriteOnceForABurst(t *testing.T) {
	a := testApp(t)
	for i := range 10 {
		a.prefs.SidebarWidth = float32(220 + i)
		a.savePrefs()
	}
	a.flushPrefs()
	if got := a.prefsOut.writes(); got != 1 {
		t.Fatalf("ten changes wrote app.json %d times", got)
	}
	if got := readPrefs(t).SidebarWidth; got != 229 {
		t.Errorf("app.json holds the width %v, want the last one, 229", got)
	}
}

// A save returns before the disk is asked: the write is the background's,
// so a session switch or a drag of the sidebar does not wait for it.
func TestPrefsWaitForTheBackground(t *testing.T) {
	a := testApp(t)
	a.prefs.SidebarWidth = 300
	a.savePrefs()
	if _, err := os.Stat(prefsPath()); !os.IsNotExist(err) {
		t.Fatalf("the save wrote app.json before it returned: %v", err)
	}
	a.flushPrefs()
	if got := readPrefs(t).SidebarWidth; got != 300 {
		t.Errorf("the flushed write holds the width %v, want 300", got)
	}
}

// A save of what the file already holds writes nothing: a session switch
// that moves nothing new leaves app.json alone.
func TestPrefsUnchangedWritesNothing(t *testing.T) {
	a := testApp(t)
	a.prefs.SidebarWidth = 300
	a.savePrefs()
	a.flushPrefs()
	if got := a.prefsOut.writes(); got != 1 {
		t.Fatalf("a change wrote app.json %d times, want once", got)
	}
	a.savePrefs()
	a.flushPrefs()
	if got := a.prefsOut.writes(); got != 1 {
		t.Errorf("a save of unchanged prefs wrote app.json again: %d writes, was 1", got)
	}
}

// Quitting writes a change made in the last moment, before the app goes,
// for the prefs and for the settings.
func TestQuitFlushesWhatWaits(t *testing.T) {
	a := testApp(t)
	a.prefs.SidebarWidth = 300
	a.savePrefs()
	s := a.set
	s.Appearance = "dark"
	a.applySettings(s)
	a.beforeQuit()
	if got := readPrefs(t).SidebarWidth; got != 300 {
		t.Errorf("app.json holds the width %v, want 300: the quit lost it", got)
	}
	if got := settings.Load().Appearance; got != "dark" {
		t.Errorf("the configuration holds the appearance %q, want dark: the quit lost it", got)
	}
}
