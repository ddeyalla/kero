package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestFormatClock(t *testing.T) {
	for in, want := range map[float64]string{-3: "0:00", 0: "0:00", 5.9: "0:05", 65: "1:05", 600: "10:00", 3725: "1:02:05"} {
		if got := FormatClock(in); got != want {
			t.Errorf("FormatClock(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAdvanceValue(t *testing.T) {
	if got := AdvanceValue(10, 1, 1000, 3500, 0, 100); got != 12.5 {
		t.Fatalf("advanced = %v, want 12.5", got)
	}
	if got := AdvanceValue(10, 1, 1000, 999000, 0, 100); got != 100 {
		t.Fatalf("clamped = %v, want 100", got)
	}
	if got := AdvanceValue(10, 0, 1000, 5000, 0, 100); got != 10 {
		t.Fatalf("no rate = %v, want 10", got)
	}
	if got := AdvanceValue(10, 1, 0, 5000, 0, 100); got != 10 {
		t.Fatalf("no instant = %v, want 10", got)
	}
	if got := AdvanceValue(10, 1, 5000, 1000, 0, 100); got != 6 {
		t.Fatalf("now before at = %v, want 6", got)
	}
}

func TestValidateTreeUsageParts(t *testing.T) {
	marker := 0.7
	good := Tree{Root: &Node{
		Type: NodeColumn, ID: "root", Surface: SurfaceCard, Radius: 12,
		Children: []*Node{
			{Type: NodeText, ID: "dot", Icon: "dot", Color: ColorOrange},
			{Type: NodeText, ID: "name", Text: "Claude", Mark: "claude", Role: RoleStrong},
			{Type: NodeText, ID: "pace", Text: "Limit in 1d", Icon: "flame", Color: ColorDanger},
			{Type: NodeProgress, ID: "bar", Value: 0.4, Marker: &marker, Color: ColorWarning},
			{Type: NodeSegmented, ID: "range", Items: []Item{
				{ID: "today", Title: "Today", Active: true}, {ID: "30d", Title: "30 Days"},
			}},
			{Type: NodeDonut, ID: "donut", Width: 96, Text: "$4.6K", Detail: "dollars", Segments: []Segment{
				{ID: "claude", Value: 3, Color: ColorOrange}, {ID: "codex", Value: 1, Color: ColorGreen},
			}},
			{Type: NodeDonut, ID: "empty", Text: "—"},
			{Type: NodeIconButton, ID: "toggle", Icon: "chevron-down"},
		},
	}}
	if err := good.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree: %v", err)
	}

	over, under := 1.5, -0.1
	nan := 0.0
	nan = nan / nan
	two := []Item{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
	bad := []struct {
		name string
		node *Node
	}{
		{"unknown surface", &Node{Type: NodeColumn, ID: "n", Surface: "glass"}},
		{"negative radius", &Node{Type: NodeRow, ID: "n", Surface: SurfaceCard, Radius: -1}},
		{"unknown text icon", &Node{Type: NodeText, ID: "n", Icon: "rocket"}},
		{"unknown mark", &Node{Type: NodeText, ID: "n", Mark: "netscape"}},
		{"unknown progress color", &Node{Type: NodeProgress, ID: "n", Color: "pink"}},
		{"marker over 1", &Node{Type: NodeProgress, ID: "n", Marker: &over}},
		{"marker under 0", &Node{Type: NodeProgress, ID: "n", Marker: &under}},
		{"marker not a number", &Node{Type: NodeProgress, ID: "n", Marker: &nan}},
		{"segmented with one item", &Node{Type: NodeSegmented, ID: "n", Items: two[:1]}},
		{"segmented with none", &Node{Type: NodeSegmented, ID: "n"}},
		{"segmented repeats an id", &Node{Type: NodeSegmented, ID: "n", Items: []Item{{ID: "a", Title: "A"}, {ID: "a", Title: "B"}}}},
		{"segmented item without title", &Node{Type: NodeSegmented, ID: "n", Items: []Item{{ID: "a", Title: "A"}, {ID: "b"}}}},
		{"segmented two active", &Node{Type: NodeSegmented, ID: "n", Items: []Item{{ID: "a", Title: "A", Active: true}, {ID: "b", Title: "B", Active: true}}}},
		{"donut negative value", &Node{Type: NodeDonut, ID: "n", Segments: []Segment{{ID: "a", Value: -1}}}},
		{"donut value not a number", &Node{Type: NodeDonut, ID: "n", Segments: []Segment{{ID: "a", Value: nan}}}},
		{"donut small", &Node{Type: NodeDonut, ID: "n", Width: 47}},
		{"donut large", &Node{Type: NodeDonut, ID: "n", Width: 241}},
		{"donut unknown color", &Node{Type: NodeDonut, ID: "n", Segments: []Segment{{ID: "a", Value: 1, Color: "pink"}}}},
		{"donut repeats an id", &Node{Type: NodeDonut, ID: "n", Segments: []Segment{{ID: "a", Value: 1}, {ID: "a", Value: 2}}}},
		{"donut bad segment id", &Node{Type: NodeDonut, ID: "n", Segments: []Segment{{ID: "a b", Value: 1}}}},
	}
	many := make([]Item, MaxSegmentedItems+1)
	for i := range many {
		many[i] = Item{ID: fmt.Sprintf("i%d", i), Title: "Item"}
	}
	bad = append(bad, []struct {
		name string
		node *Node
	}{
		{"segmented with too many items", &Node{Type: NodeSegmented, ID: "n", Items: many}},
		{"surface on a text", &Node{Type: NodeText, ID: "n", Surface: SurfaceCard}},
		{"marker on a text", &Node{Type: NodeText, ID: "n", Marker: &marker}},
		{"mark on a button", &Node{Type: NodeIconButton, ID: "n", Icon: "play", Mark: "claude"}},
		{"segments on a list", &Node{Type: NodeList, ID: "n", Segments: []Segment{{ID: "a", Value: 1}}}},
	}...)
	if err := (&Tree{Root: &Node{Type: NodeSegmented, ID: "n", Items: many[:MaxSegmentedItems]}}).ValidateTree(); err != nil {
		t.Fatalf("a segmented control of %d items: %v", MaxSegmentedItems, err)
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if err := (&Tree{Root: c.node}).ValidateTree(); err == nil {
				t.Fatal("ValidateTree = nil, want an error")
			}
		})
	}
}

// The new fields cross the wire, and a node without them is as before.
func TestUsagePartsJSON(t *testing.T) {
	in := `{"type":"progress","id":"bar","value":0.4,"marker":0,"color":"danger"}`
	var n Node
	if err := json.Unmarshal([]byte(in), &n); err != nil {
		t.Fatal(err)
	}
	if n.Marker == nil || *n.Marker != 0 {
		t.Fatalf("marker = %v, want a marker at zero kept", n.Marker)
	}
	out, err := json.Marshal(&Node{Type: NodeText, ID: "t", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"type":"text","id":"t","text":"x"}` {
		t.Fatalf("a plain text marshals as %s", out)
	}
}

func TestValidateTreeBoardParts(t *testing.T) {
	good := Tree{Root: &Node{
		Type: NodeColumn, ID: "root",
		Children: []*Node{
			{Type: NodeRow, ID: "cols", ScrollX: true, Gap: 12, Children: []*Node{
				{Type: NodeColumn, ID: "card", Pressable: true, Label: "BRU-1", Surface: SurfaceCard, Width: 260},
			}},
			{Type: NodeMenu, ID: "assignee", Label: "Assignee", Icon: "person", Items: []Item{
				{ID: "me", Title: "Me", Active: true}, {ID: "anyone", Title: "Anyone", Active: true},
			}},
			{Type: NodeMarkdown, ID: "body", Text: "# Title\n\nA [link](https://example.com)."},
			{Type: NodeText, ID: "p", Icon: "priority-urgent"},
			{Type: NodeText, ID: "s1", Icon: "circle-dashed"},
			{Type: NodeText, ID: "s2", Icon: "circle-half"},
			{Type: NodeText, ID: "s3", Icon: "check-circle"},
			{Type: NodeText, ID: "s4", Icon: "xmark-circle"},
			{Type: NodeText, ID: "i1", Icon: "filter"},
			{Type: NodeText, ID: "i2", Icon: "person"},
			{Type: NodeText, ID: "i3", Icon: "tag"},
			{Type: NodeText, ID: "i4", Icon: "flag"},
			{Type: NodeText, ID: "i5", Icon: "pin"},
			{Type: NodeText, ID: "i6", Icon: "circle"},
		},
	}}
	if err := good.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree: %v", err)
	}

	items := make([]Item, MaxMenuItems+1)
	for i := range items {
		items[i] = Item{ID: fmt.Sprintf("i%d", i), Title: "Item"}
	}
	one := []Item{{ID: "a", Title: "A"}}
	if err := (&Tree{Root: &Node{Type: NodeMenu, ID: "n", Label: "Menu", Items: items[:MaxMenuItems]}}).ValidateTree(); err != nil {
		t.Fatalf("a menu of %d items: %v", MaxMenuItems, err)
	}
	bad := []struct {
		name string
		node *Node
	}{
		{"menu without a label", &Node{Type: NodeMenu, ID: "n", Items: one}},
		{"menu without items", &Node{Type: NodeMenu, ID: "n", Label: "Menu"}},
		{"menu with too many items", &Node{Type: NodeMenu, ID: "n", Label: "Menu", Items: items}},
		{"menu repeats an id", &Node{Type: NodeMenu, ID: "n", Label: "Menu", Items: []Item{{ID: "a", Title: "A"}, {ID: "a", Title: "B"}}}},
		{"menu item without a title", &Node{Type: NodeMenu, ID: "n", Label: "Menu", Items: []Item{{ID: "a"}}}},
		{"menu item with a bad id", &Node{Type: NodeMenu, ID: "n", Label: "Menu", Items: []Item{{ID: "a b", Title: "A"}}}},
		{"menu with an unknown icon", &Node{Type: NodeMenu, ID: "n", Label: "Menu", Icon: "rocket", Items: one}},
		{"markdown too long", &Node{Type: NodeMarkdown, ID: "n", Text: strings.Repeat("a", MaxMarkdown+1)}},
		{"markdown with children", &Node{Type: NodeMarkdown, ID: "n", Children: []*Node{{Type: NodeDivider, ID: "d"}}}},
		{"pressable text", &Node{Type: NodeText, ID: "n", Text: "x", Pressable: true}},
		{"pressable button", &Node{Type: NodeButton, ID: "n", Label: "Go", Pressable: true}},
		{"scrollX column", &Node{Type: NodeColumn, ID: "n", ScrollX: true}},
		{"scrollX list", &Node{Type: NodeList, ID: "n", ScrollX: true}},
		{"menu with children", &Node{Type: NodeMenu, ID: "n", Label: "Menu", Items: one, Children: []*Node{{Type: NodeDivider, ID: "d"}}}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if err := (&Tree{Root: c.node}).ValidateTree(); err == nil {
				t.Fatal("ValidateTree = nil, want an error")
			}
		})
	}
	for _, ok := range []*Node{
		{Type: NodeRow, ID: "n", Pressable: true},
		{Type: NodeColumn, ID: "n", Pressable: true},
		{Type: NodeRow, ID: "n", ScrollX: true},
		{Type: NodeMarkdown, ID: "n", Text: strings.Repeat("a", MaxMarkdown)},
	} {
		if err := (&Tree{Root: ok}).ValidateTree(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
}

// The new fields are additive: a node without them marshals as before,
// and a node with them crosses the wire.
func TestBoardPartsJSON(t *testing.T) {
	out, err := json.Marshal(&Node{Type: NodeRow, ID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"type":"row","id":"r"}` {
		t.Fatalf("a plain row marshals as %s", out)
	}
	var n Node
	if err := json.Unmarshal([]byte(`{"type":"row","id":"r","pressable":true,"scrollX":true}`), &n); err != nil {
		t.Fatal(err)
	}
	if !n.Pressable || !n.ScrollX {
		t.Fatalf("node = %+v", n)
	}
}

func scrollTree(header, root *Node, keys ...string) *Tree {
	return &Tree{Root: root, Scroll: true, Header: header, Keys: keys}
}

func TestHeaderNeedsScrollAndSharesIDs(t *testing.T) {
	head := &Node{Type: NodeRow, ID: "head", Children: []*Node{{Type: NodeText, ID: "title", Text: "Issues"}}}
	body := &Node{Type: NodeColumn, ID: "root", Children: []*Node{{Type: NodeText, ID: "row", Text: "BRU-1"}}}
	if err := scrollTree(head, body).ValidateTree(); err != nil {
		t.Fatalf("a header over a scrolling pane is valid: %v", err)
	}
	if err := (&Tree{Root: body, Header: head}).ValidateTree(); err == nil || !strings.Contains(err.Error(), "header needs") {
		t.Fatalf("a header without scroll = %v, want a refusal", err)
	}
	clash := &Node{Type: NodeRow, ID: "head", Children: []*Node{{Type: NodeText, ID: "row", Text: "x"}}}
	if err := scrollTree(clash, body).ValidateTree(); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("an id in the header and the root = %v, want it repeated", err)
	}
	bad := &Node{Type: NodeText, ID: "head", Icon: "no-such-icon", Text: "x"}
	if err := scrollTree(bad, body).ValidateTree(); err == nil {
		t.Fatal("the header is checked like the root")
	}
}

func TestKeysAreKnownAndOnce(t *testing.T) {
	root := &Node{Type: NodeColumn, ID: "root"}
	if err := scrollTree(nil, root, "j", "k", "up", "down", "left", "right", "enter", "escape").ValidateTree(); err != nil {
		t.Fatalf("every known key is valid: %v", err)
	}
	for name, keys := range map[string][]string{
		"unknown":  {"j", "f1"},
		"repeated": {"j", "j"},
		"upper":    {"J"},
	} {
		if err := scrollTree(nil, root, keys...).ValidateTree(); err == nil {
			t.Errorf("%s keys %v were accepted", name, keys)
		}
	}
	if err := scrollTree(nil, root, "j", "k", "up", "down", "left", "right", "enter", "escape", "j").ValidateTree(); err == nil {
		t.Error("more keys than the protocol knows were accepted")
	}
}

func TestContextMenu(t *testing.T) {
	menu := []Item{
		{ID: "open", Title: "Open"},
		{Separator: true},
		{ID: "move", Title: "Move to", Items: []Item{{ID: "mv:todo", Title: "Todo", Active: true}, {ID: "mv:done", Title: "Done"}}},
	}
	card := func(mod func(n *Node)) Tree {
		n := &Node{Type: NodeColumn, ID: "card", Pressable: true, ContextMenu: menu}
		if mod != nil {
			mod(n)
		}
		return Tree{Root: &Node{Type: NodeColumn, ID: "root", Children: []*Node{n}}}
	}
	if err := (func() *Tree { t := card(nil); return &t })().ValidateTree(); err != nil {
		t.Fatalf("a context menu on a pressable column is valid: %v", err)
	}
	for name, mod := range map[string]func(n *Node){
		"not pressable": func(n *Node) { n.Pressable = false },
		"repeated id":   func(n *Node) { n.ContextMenu = []Item{{ID: "a", Title: "A"}, {ID: "a", Title: "B"}} },
		"repeated in a submenu": func(n *Node) {
			n.ContextMenu = []Item{{ID: "a", Title: "A"}, {ID: "s", Title: "S", Items: []Item{{ID: "a", Title: "A"}}}}
		},
		"no title": func(n *Node) { n.ContextMenu = []Item{{ID: "a"}} },
		"bad id":   func(n *Node) { n.ContextMenu = []Item{{ID: "a b", Title: "A"}} },
		"nested": func(n *Node) {
			n.ContextMenu = []Item{{ID: "s", Title: "S", Items: []Item{{ID: "t", Title: "T", Items: []Item{{ID: "u", Title: "U"}}}}}}
		},
		"separator with an id": func(n *Node) { n.ContextMenu = []Item{{ID: "a", Separator: true}} },
		"too many": func(n *Node) {
			n.ContextMenu = nil
			for i := 0; i <= MaxContextItems; i++ {
				n.ContextMenu = append(n.ContextMenu, Item{ID: fmt.Sprintf("i%d", i), Title: "x"})
			}
		},
	} {
		tree := card(mod)
		if err := tree.ValidateTree(); err == nil {
			t.Errorf("%s: the context menu was accepted", name)
		}
	}
	// A list, a menu and a segmented control take no separator or submenu.
	for _, n := range []*Node{
		{Type: NodeList, ID: "l", Items: []Item{{ID: "a", Title: "A", Items: []Item{{ID: "b", Title: "B"}}}}},
		{Type: NodeMenu, ID: "m", Label: "M", Items: []Item{{ID: "a", Title: "A", Separator: true}}},
		{Type: NodeSegmented, ID: "s", Items: []Item{{ID: "a", Title: "A", Separator: true}, {ID: "b", Title: "B"}}},
	} {
		tree := Tree{Root: &Node{Type: NodeColumn, ID: "root", Children: []*Node{n}}}
		if err := tree.ValidateTree(); err == nil {
			t.Errorf("a %s took a separator or a submenu", n.Type)
		}
	}
}

func TestChipHexAndScrollY(t *testing.T) {
	text := func(mod func(n *Node)) Tree {
		n := &Node{Type: NodeText, ID: "t", Text: "bug"}
		mod(n)
		return Tree{Root: &Node{Type: NodeRow, ID: "root", Children: []*Node{n}}}
	}
	for _, chip := range []string{ChipNeutral, "#E5484D", "#e5484d"} {
		tree := text(func(n *Node) { n.Chip = chip; n.Hex = "#2F6FED" })
		if err := tree.ValidateTree(); err != nil {
			t.Errorf("chip %q: %v", chip, err)
		}
	}
	for _, bad := range []string{"red", "#fff", "#12345", "#GGGGGG", "E5484D", "#E5484DFF"} {
		tree := text(func(n *Node) { n.Chip = bad })
		if err := tree.ValidateTree(); err == nil {
			t.Errorf("chip %q was accepted", bad)
		}
		tree = text(func(n *Node) { n.Hex = bad })
		if err := tree.ValidateTree(); err == nil {
			t.Errorf("hex %q was accepted", bad)
		}
	}
	notText := Tree{Root: &Node{Type: NodeRow, ID: "root", Chip: ChipNeutral}}
	if err := notText.ValidateTree(); err == nil {
		t.Error("a row took a chip")
	}

	side := Tree{Root: &Node{Type: NodeRow, ID: "root", Children: []*Node{
		{Type: NodeColumn, ID: "main", ScrollY: true, Grow: 1},
		{Type: NodeColumn, ID: "side", ScrollY: true, Width: 420},
	}}}
	if err := side.ValidateTree(); err != nil {
		t.Fatalf("columns that scroll side by side are valid: %v", err)
	}
	row := Tree{Root: &Node{Type: NodeRow, ID: "root", ScrollY: true}}
	if err := row.ValidateTree(); err == nil {
		t.Error("a row scrolled up and down")
	}
}

func TestIssueIconsAreKnown(t *testing.T) {
	for _, icon := range []string{"milestone", "blocked", "blocking", "subissue"} {
		tree := Tree{Root: &Node{Type: NodeText, ID: "t", Icon: icon}}
		if err := tree.ValidateTree(); err != nil {
			t.Errorf("icon %q: %v", icon, err)
		}
	}
}
