package proto

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestLocalEditionHasSeparateData(t *testing.T) {
	name := mygo.App.Name()
	t.Cleanup(func() { mygo.App.SetName(name) })
	t.Setenv("KERO_HOME", "")
	mygo.App.SetName("Kero")
	upstream := Dir()
	mygo.App.SetName("Kero Local")
	want := "kero-local"
	if mygo.IsDev() {
		want += "-dev"
	}
	if Dir() == upstream || filepath.Base(Dir()) != want {
		t.Fatalf("local edition shares upstream data: %s", Dir())
	}
}
