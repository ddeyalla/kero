package main

import (
	"fmt"
	"image"
	"math"
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
							toolbarGroup(c, "tools", &lightPalette, func() {
								ui.Box(c).Size(216, 24).Label("capsule")
							})
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
				if kind == "toolbar" {
					// The label is on the content, inset two points by the group.
					r = ui.Rect{X: r.X - 2, Y: r.Y - 2, W: r.W + 4, H: r.H + 4}
				}
				for _, point := range []image.Point{
					{X: int(r.X + r.W/2), Y: int(r.Y)},
					{X: int(r.X + r.W/2), Y: int(r.Y + r.H - 1)},
					{X: int(r.X), Y: int(r.Y + r.H/2)},
					{X: int(math.Ceil(float64(r.X+r.W))) - 1, Y: int(r.Y + r.H/2)},
				} {
					p := tt.Image().RGBAAt(point.X, point.Y)
					if p.R > 250 && p.G > 250 && p.B > 250 {
						t.Errorf("capsule edge disappears at %v: %v", point, p)
					}
				}
			})
		}
	}
}

// Search fields already own a one-point rim. Adding the toolbar material
// must not stack another hairline onto that existing boundary.
func TestGlassToolbarKeepsExistingFieldOutline(t *testing.T) {
	var baseline [2]uint8
	for _, material := range []bool{false, true} {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Padding(16).Background(ui.Hex("#ffffff")).Children(func() {
				e := field(c, ui.Row(c).Width(180).Label("search"), false, &lightPalette)
				if material {
					glassToolbar(c, e, &lightPalette)
				}
			})
		}, 240, 64)
		tt.Frame()
		r, _ := tt.Find("search")
		for i, y := range []int{int(r.Y), int(r.Y + r.H - 1)} {
			got := tt.Image().RGBAAt(int(r.X+r.W/2), y).R
			if !material {
				baseline[i] = got
			} else if got != baseline[i] {
				t.Errorf("material changed field rim at y=%d: %d vs %d", y, got, baseline[i])
			}
		}
	}
}
