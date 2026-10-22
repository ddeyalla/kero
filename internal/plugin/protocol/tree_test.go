package protocol

import (
	"encoding/json"
	"fmt"
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
