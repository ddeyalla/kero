package main

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
)

// testerAccessNode is the part of MyGo's accessibility tree a pane test
// needs. The tester keeps the tree in an unexported field; this pinned
// dependency has no public reader, so the test reads its published shape
// without adding a production hook.
type testerAccessNode struct {
	parent int
	role   uint8
	label  string
}

// testerAccessNode.role is the platform AccessRole MyGo stores, which
// ui/access.go derives as e.role - ui.RoleGroup. RoleGroup is not zero in
// ui.Role, so a direct uint8(ui.RoleGroup) would be 2, not the stored 0.
// Subtract the public group role from each public role here instead.
const (
	accessGroup     uint8 = uint8(ui.RoleGroup - ui.RoleGroup)
	accessButton    uint8 = uint8(ui.RoleButton - ui.RoleGroup)
	accessSlider    uint8 = uint8(ui.RoleSlider - ui.RoleGroup)
	accessTextField uint8 = uint8(ui.RoleTextField - ui.RoleGroup)
)

// viewWithUnrelatedGroup draws an unrelated accessible group beside the app,
// so a test cannot pass merely because some group exists somewhere.
func viewWithUnrelatedGroup(a *App) func(c *ui.Context) {
	return func(c *ui.Context) {
		a.view(c)
		ui.Box(c.Key("unrelated-group")).Absolute().Top(0).Left(0).Size(2, 2).
			Role(ui.RoleGroup).Label("Unrelated group")
	}
}

func accessIndex(nodes []testerAccessNode, role uint8, label string) int {
	for i, n := range nodes {
		if n.role == role && n.label == label {
			return i
		}
	}
	return -1
}

func accessDescendant(nodes []testerAccessNode, ancestor, child int) bool {
	for child >= 0 {
		if child == ancestor {
			return true
		}
		child = nodes[child].parent
	}
	return false
}

func testerAccessibilityOn(t *testing.T, tt *ui.Tester) {
	t.Helper()
	rt := reflect.ValueOf(tt).Elem().FieldByName("rt")
	if rt.IsNil() {
		t.Fatal("the tester has no runtime")
	}
	field, ok := rt.Type().Elem().FieldByName("access")
	if !ok || field.Type.Kind() != reflect.Bool {
		t.Fatal("the pinned MyGo tester no longer has an accessibility switch")
	}
	reflect.NewAt(field.Type, unsafe.Pointer(uintptr(rt.UnsafePointer())+field.Offset)).Elem().SetBool(true)
}

func testerAccessNodes(t *testing.T, tt *ui.Tester) []testerAccessNode {
	t.Helper()
	h := reflect.ValueOf(tt).Elem().FieldByName("h")
	if h.IsNil() {
		t.Fatal("the tester has no hosted runtime")
	}
	headless := h.Type().Elem()
	field, ok := headless.FieldByName("access")
	if !ok {
		t.Fatal("the pinned MyGo tester no longer keeps an accessibility tree")
	}
	tree := reflect.NewAt(field.Type, unsafe.Pointer(uintptr(h.UnsafePointer())+field.Offset)).Elem()
	if tree.IsNil() {
		t.Fatal("no accessibility tree was built")
	}
	nodes := tree.Elem().FieldByName("Nodes")
	out := make([]testerAccessNode, 0, nodes.Len())
	for i := 0; i < nodes.Len(); i++ {
		n := nodes.Index(i)
		out = append(out, testerAccessNode{
			parent: int(n.FieldByName("Parent").Int()),
			role:   uint8(n.FieldByName("Role").Uint()),
			label:  n.FieldByName("Label").String(),
		})
	}
	return out
}

// A plugin body is a group, not one button around every control: its
// button, search field and slider stay distinct accessible elements. The
// same body still owns pane focus and hands plugin keys back when another
// pane takes focus.
func TestViewPaneKeepsNestedPluginControlsAccessible(t *testing.T) {
	a := testApp(t)
	a.plugins.knownAt = true
	a.plugins.known = []registry.Plugin{{Manifest: fakePluginManifest(), Builtin: true}}
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: "fake", Widget: "main"})
	if err != nil {
		t.Fatal(err)
	}
	p := &a.st.Sessions[0].Panes[1]
	p.View, p.Target = proto.PanePlugins, target
	st := &pluginPaneState{
		id:     "b",
		plugin: "fake",
		widget: "main",
		tree: pluginTree(sdk.Column("root",
			sdk.Button("signin", "Sign In").Primary(),
			sdk.SearchField("search", "Search files"),
			sdk.Slider("volume", 0.5, 0, 1).Size(160, 0).Step(0.1),
		)),
		view:    newPluginView(),
		opened:  true,
		visible: true,
		size:    protocol.Size{Width: 1, Height: 1},
	}
	a.plugins.panes = map[string]*pluginPaneState{"b": st}
	tt := ui.NewTester(viewWithUnrelatedGroup(a), 1000, 700)
	testerAccessibilityOn(t, tt)
	frames(tt)

	nodes := testerAccessNodes(t, tt)
	body := accessIndex(nodes, accessGroup, paneViewName(proto.PanePlugins))
	unrelated := accessIndex(nodes, accessGroup, "Unrelated group")
	if body < 0 {
		t.Fatalf("the plugin body is not a named accessible group: %+v", nodes)
	}
	if unrelated < 0 || unrelated == body {
		t.Fatalf("the unrelated control group is missing or ambiguous: %+v", nodes)
	}
	button := accessIndex(nodes, accessButton, "Sign In")
	search := accessIndex(nodes, accessTextField, "Search files")
	slider := accessIndex(nodes, accessSlider, "Slider")
	for name, i := range map[string]int{"Sign In": button, "Search files": search, "Slider": slider} {
		if i < 0 {
			t.Errorf("the nested %s control is missing: %+v", name, nodes)
			continue
		}
		if !accessDescendant(nodes, body, i) || accessDescendant(nodes, unrelated, i) {
			t.Errorf("the nested %s control is not under the plugin body group: %+v", name, nodes)
		}
	}

	if err := tt.Click("Search files"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.Focused("Search files") {
		t.Fatal("the search field did not take keyboard focus")
	}
	a.setFocus(a.session("s1"), "b")
	frames(tt)
	if !st.view.keysOn {
		t.Fatal("the focused plugin pane did not hand keys to the plugin")
	}
	a.setFocus(a.session("s1"), "a")
	frames(tt)
	if st.view.keysOn {
		t.Fatal("the plugin kept its keys after another pane took focus")
	}
}

// The same shared body is a group for built-in views too.
func TestViewPaneBodyIsGroupForBuiltinAttention(t *testing.T) {
	a := testApp(t)
	p := &a.st.Sessions[0].Panes[1]
	p.View, p.Shell, p.Target = proto.PaneAttention, "", ""
	tt := ui.NewTester(viewWithUnrelatedGroup(a), 1000, 700)
	testerAccessibilityOn(t, tt)
	frames(tt)
	nodes := testerAccessNodes(t, tt)
	body := accessIndex(nodes, accessGroup, paneViewName(proto.PaneAttention))
	unrelated := accessIndex(nodes, accessGroup, "Unrelated group")
	if body < 0 {
		t.Fatalf("the built-in view body is not a named accessible group: %+v", nodes)
	}
	if unrelated < 0 || unrelated == body {
		t.Fatalf("the unrelated control group is missing or ambiguous: %+v", nodes)
	}
}
