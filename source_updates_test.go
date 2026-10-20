package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSourceUpdateButtonKeepsCurrentSessionDuringBuild(t *testing.T) {
	a := testApp(t)
	a.maintenance.busy = true
	selected := a.selected
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	if err := tt.Click("Update Shibu from Kero"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.selected != selected || !a.maintenance.busy {
		t.Fatal("an update click interrupted the current session or started a duplicate build")
	}
}
