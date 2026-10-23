package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"kero/locales"
)

func TestSourceUpdatesMovedToSettings(t *testing.T) {
	a := settingsTestApp(t)
	tt := ui.NewTester(a.view, 1100, 1000)
	tt.Frame()
	if _, ok := tt.Find("Update Shibu from Kero"); ok {
		t.Fatal("source updater still in sidebar")
	}
	// Render the actual Updates group at a stable size; modal navigation is
	// covered separately, and this group lives on its General page.
	tt = ui.NewTester(func(c *ui.Context) { a.updatesGroup(c, a.paletteFor(c)) }, 800, 400)
	tt.Frame()
	for _, label := range []string{locales.SetCheckForUpdates(), locales.SetSourceBuild()} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("missing Settings action %q; texts %q", label, tt.Texts())
		}
	}
	a.maintenance.busy = true
	a.maintenance.status = "Checking Kero and building Shibu…"
	tt.Frame()
	if _, ok := tt.Find(locales.SetSourceBuild()); ok {
		t.Fatal("duplicate source build can be clicked")
	}
	if !tt.HasText(locales.SetSourceBuilding()) {
		t.Fatal("busy state missing")
	}
	selected := a.selected
	a.run("updates.sync", nil)
	if a.selected != selected || !a.maintenance.busy {
		t.Fatal("duplicate check interrupted session")
	}
	a.maintenance.busy = false
	a.maintenance.status = "Update needs repair. Current app unchanged."
	tt.Frame()
	if _, ok := tt.Find(locales.SetSourceBuild()); !ok {
		t.Fatal("retry action missing")
	}
	if !tt.HasText(locales.SetSourceUpdatesDetail() + "\n" + a.maintenance.status) {
		t.Fatal("result not shown")
	}
	shot(t, tt, "source-updates-settings")
}
