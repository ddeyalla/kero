package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"kero/internal/plugin/protocol"
)

// On a white pane, the glass's lit horizontal rim blends into its
// backdrop. Controls still need a complete boundary, not just two ends.
func TestGlassCapsuleEdgesOnWhite(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		for _, kind := range []string{"toolbar", "filter"} {
			t.Run(fmt.Sprintf("%s/reduced-%v", kind, reduced), func(t *testing.T) {
				v := newPluginView()
				v.controls = make(map[string]uint64)
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Padding(16).Background(ui.Hex("#ffffff")).AlignItems(ui.Start).Children(func() {
						if kind == "toolbar" {
							glassToolbar(c, ui.Row(c).Size(220, 28).Label("capsule"), &lightPalette)
						} else {
							v.menu(c, &lightPalette, &protocol.Node{ID: "filter", Label: "capsule"}, true, func(pluginEvent) {})
						}
					})
				}, 260, 64)
				tt.SetPreferences(ui.Preferences{TextScale: 1, ReduceTransparency: reduced})
				tt.Frame()
				shot(t, tt, fmt.Sprintf("capsule-%s-reduced-%v", kind, reduced))
				r, ok := tt.Find("capsule")
				if !ok {
					t.Fatal("missing capsule")
				}
				for _, y := range []int{int(r.Y), int(r.Y + r.H - 1)} {
					p := tt.Image().RGBAAt(int(r.X+r.W/2), y)
					if p.R > 250 && p.G > 250 && p.B > 250 {
						t.Errorf("horizontal capsule edge disappears at y=%d: %v", y, p)
					}
				}
			})
		}
	}
}
