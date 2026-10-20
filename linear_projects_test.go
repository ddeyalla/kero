package main

import (
	"testing"

	"kero/internal/proto"
)

func TestLinearSuggest(t *testing.T) {
	lps := []proto.LinearProject{{ID: "a", Name: "Shibu Code"}, {ID: "b", Name: "Rig"}, {ID: "c", Name: "Brunoh Web"}, {ID: "d", Name: "Brunoh API"}}
	for _, c := range []struct {
		name, dir, want string
	}{
		{"kero-deyalla", "/code/shibu-code", "a"},
		{"shibu code", "", "a"},
		{"rig", "/x/rig", "b"},
		{"brunoh-web", "/x/brunoh-web", "c"},
		{"brunoh", "/x/brunoh", ""}, // two projects contain it
		{"misc", "/x/misc", ""},
	} {
		if got := linearSuggest(proto.Project{Name: c.name, Dir: c.dir}, lps); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.name, c.dir, got, c.want)
		}
	}
	if got := linearProjectsFor(lps, "c"); got[0].ID != "c" || got[1].ID != "d" {
		t.Errorf("order: %v", got)
	}
}
