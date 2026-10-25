package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
)

// The empty shell is its identity and its two actions. Helper prose and the
// agent examples stay in the CLI guides, not on the screen.
func TestEmptyShellHasOnlyRealActions(t *testing.T) {
	previous := mygo.App.Name()
	mygo.App.SetName("Shibu Code")
	t.Cleanup(func() { mygo.App.SetName(previous) })

	a := testApp(t)
	a.st = proto.State{}
	a.loaded, a.selected = true, ""
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		for _, size := range []struct{ width, height int }{{560, 360}, {560, 700}, {900, 700}} {
			width, height := size.width, size.height
			tt := ui.NewTester(a.view, width, height)
			tt.SetDark(dark)
			tt.Frame()
			if _, ok := tt.Find(mygo.App.Name()); !ok {
				t.Fatalf("%s %dx%d: no Shibu Code identity; texts %q", name, width, height, tt.Texts())
			}
			title, ok := tt.Find(locales.EmptyTitle())
			if !ok {
				t.Fatalf("%s %dx%d: no empty title; texts %q", name, width, height, tt.Texts())
			}
			actions := []struct {
				label string
				key   string
			}{
				{locales.ActionNewSession(), a.keys("session.new")},
				{locales.ActionOpenProject(), a.keys("project.open")},
			}
			var rects []ui.Rect
			for _, action := range actions {
				r, ok := tt.Find(action.label)
				if !ok {
					t.Fatalf("%s %dx%d: no %s action; texts %q", name, width, height, action.label, tt.Texts())
				}
				if r.X < 0 || r.X+r.W > float32(width) {
					t.Fatalf("%s %dx%d: %s action is outside the window: %+v", name, width, height, action.label, r)
				}
				if action.key != "" && !tt.HasText(action.key) {
					t.Fatalf("%s %dx%d: %s action lost its shortcut %q", name, width, height, action.label, action.key)
				}
				rects = append(rects, r)
			}
			newAction, _ := tt.Find(locales.ActionNewSession())
			gap := newAction.Y - (title.Y + title.H)
			if gap < 0 || gap > 32 {
				t.Fatalf("%s %dx%d: title and actions are %v apart", name, width, height, gap)
			}
			// A wrapped row has two lines. Looking at each line's actual
			// content bounds, not the row or the window, catches start
			// alignment that AlignContent(Center) alone would leave.
			lines := map[int]*[2]float32{}
			for _, r := range rects {
				line := int(r.Y)
				bounds, ok := lines[line]
				if !ok {
					bounds = &[2]float32{r.X, r.X + r.W}
					lines[line] = bounds
				} else {
					bounds[0] = min(bounds[0], r.X)
					bounds[1] = max(bounds[1], r.X+r.W)
				}
			}
			if width == 560 && len(lines) < 2 {
				t.Fatalf("%s %d: the actions did not wrap into two lines: %+v", name, width, rects)
			}
			center := title.X + title.W/2
			for _, bounds := range lines {
				if got := (bounds[0] + bounds[1]) / 2; got < center-1.5 || got > center+1.5 {
					t.Fatalf("%s %dx%d: action line center %v, content center %v; rects %+v", name, width, height, got, center, rects)
				}
			}
			for _, removed := range []string{"For agents", "kero new -n review", "Sessions keep running"} {
				if tt.HasText(removed) {
					t.Fatalf("%s %dx%d: removed empty-shell copy is still shown: %q", name, width, height, removed)
				}
			}
			shot(t, tt, fmt.Sprintf("empty-shell-%s-%dx%d", name, width, height))
		}
	}
}
