package protocol

import (
	"fmt"
	"math"
	"regexp"
	"slices"
)

// The parts of a tree that a plugin draws, one type for all of them so
// that the app can read a tree from any plugin version.
const (
	NodeRow         = "row"
	NodeColumn      = "column"
	NodeText        = "text"
	NodeImage       = "image"
	NodeButton      = "button"
	NodeIconButton  = "iconButton"
	NodeSlider      = "slider"
	NodeProgress    = "progress"
	NodeList        = "list"
	NodeSearchField = "searchField"
	NodeEmptyState  = "emptyState"
	NodeSpacer      = "spacer"
	NodeDivider     = "divider"
	NodeSegmented   = "segmented"
	NodeDonut       = "donut"
)

// KnownNodeTypes are the part types this protocol version knows.
var KnownNodeTypes = []string{
	NodeRow, NodeColumn, NodeText, NodeImage, NodeButton,
	NodeIconButton, NodeSlider, NodeProgress, NodeList,
	NodeSearchField, NodeEmptyState, NodeSpacer, NodeDivider,
	NodeSegmented, NodeDonut,
}

// The roles a text part may take. A headline is the large title of a
// pane that has the room, such as the track a player shows. Strong is a
// body-sized text in a heavier weight, such as a label in a card.
const (
	RoleHeadline = "headline"
	RoleTitle    = "title"
	RoleBody     = "body"
	RoleCaption  = "caption"
	RoleMono     = "mono"
	RoleStrong   = "strong"
)

// KnownRoles are the text roles the app draws.
var KnownRoles = []string{RoleHeadline, RoleTitle, RoleBody, RoleCaption, RoleMono, RoleStrong}

// The colors a part may take: the app's own, never a free one. The first
// three are the text colors. Danger, warning and success are meanings the
// app colors for the appearance; the hues (orange to gray) are for telling
// the slices of a chart apart.
const (
	ColorText    = "text"
	ColorMuted   = "muted"
	ColorAccent  = "accent"
	ColorDanger  = "danger"
	ColorWarning = "warning"
	ColorSuccess = "success"
	ColorOrange  = "orange"
	ColorGreen   = "green"
	ColorTeal    = "teal"
	ColorBlue    = "blue"
	ColorPurple  = "purple"
	ColorGray    = "gray"
)

// KnownColorRoles are the colors the app draws.
var KnownColorRoles = []string{
	ColorText, ColorMuted, ColorAccent,
	ColorDanger, ColorWarning, ColorSuccess,
	ColorOrange, ColorGreen, ColorTeal, ColorBlue, ColorPurple, ColorGray,
}

// SurfaceCard is a row or column drawn as a card: a soft panel with
// rounded corners that sets its children apart from the pane.
const SurfaceCard = "card"

// KnownSurfaces are the surfaces a row or column may take.
var KnownSurfaces = []string{SurfaceCard}

// KnownMarks are the logos a text may show before its words: those of the
// programs the app has a logo for, by the name kerod gives them.
var KnownMarks = []string{
	"claude", "codex", "gemini", "opencode", "amp", "cursor-agent",
	"copilot", "qwen", "kimi", "cline", "node", "bun", "deno", "python",
	"python3", "vim", "nvim", "git", "docker",
}

// The alignments a row or column may take.
const (
	AlignStart        = "start"
	AlignCenter       = "center"
	AlignEnd          = "end"
	AlignSpaceBetween = "spaceBetween"
)

// KnownAligns are the alignments the app draws.
var KnownAligns = []string{AlignStart, AlignCenter, AlignEnd, AlignSpaceBetween}

// The variants a list may take. A grid draws its items as pictures with
// their title under them, each as wide as the list's width, and wraps
// them into rows: the plugin, which knows the pane's size, chooses the
// width that fills the row.
const VariantGrid = "grid"

// KnownVariants are the list variants the app draws; "" is rows.
var KnownVariants = []string{VariantGrid}

// The event kinds a part sends, with the id of the part the user used.
const (
	KindPress  = "press"
	KindChange = "change"
	KindCommit = "commit"
	KindSelect = "select"
	KindSubmit = "submit"
)

// KnownIcons are the icons a part may name. The app draws its own glyph
// for each, in the current color.
var KnownIcons = []string{
	"play", "pause", "next", "previous", "search", "close",
	"refresh", "plus", "chevron-right", "chevron-up", "chevron-down",
	"folder", "external", "copy", "link", "gear", "files", "globe",
	"terminal", "image", "back", "forward", "preview", "source",
	"doc", "list", "board", "shuffle", "repeat", "volume", "volume-off",
	"flame", "dot", "warning", "info",
}

// The limits a tree is held to, so that a plugin cannot stall the app.
const (
	MaxNodes = 2000
	MaxDepth = 24
	// MaxSegments is how many slices a donut may have.
	MaxSegments = 32
	// MaxSegmentedItems is how many choices a segmented control may have.
	MaxSegmentedItems = 8
	// MinDonut and MaxDonut bound a donut's diameter in points.
	MinDonut = 48
	MaxDonut = 240
)

var nodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)

// Node is one part of a tree. The app reads only the fields of the
// node's own type, and the tree carries a little more than v1 draws so
// that a later version can add to the same type.
type Node struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	// A row or column holds children; a list holds items instead.
	Children []*Node `json:"children,omitempty"`
	Items    []Item  `json:"items,omitempty"`

	// Text is a text's words, and a searchField's query from the plugin,
	// which the app shows when the field first appears or the query
	// changes.
	Text    string `json:"text,omitempty"`
	Role    string `json:"role,omitempty"`
	Color   string `json:"color,omitempty"`
	Lines   int    `json:"lines,omitempty"`
	Marquee bool   `json:"marquee,omitempty"` // BRU-169

	URL  string `json:"url,omitempty"`
	Data string `json:"data,omitempty"`
	// Width is a size in points. A horizontal slider with a width keeps it;
	// one without grows to fill the room of its row.
	Width  int  `json:"width,omitempty"`
	Height int  `json:"height,omitempty"`
	Radius int  `json:"radius,omitempty"`
	Tint   bool `json:"tint,omitempty"` // BRU-167

	Label string `json:"label,omitempty"`
	// Icon is an icon button's glyph, and a text's, drawn before its words
	// in the text's color. Mark is a program's logo drawn there instead.
	Icon    string `json:"icon,omitempty"`
	Mark    string `json:"mark,omitempty"`
	Primary bool   `json:"primary,omitempty"`
	// Active marks an icon button as switched on, such as shuffle that is
	// on: the app draws its glyph in the accent color.
	Active      bool   `json:"active,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"` // nil is enabled
	Action      string `json:"action,omitempty"`  // an empty state's one button
	Placeholder string `json:"placeholder,omitempty"`

	Value float64 `json:"value,omitempty"`
	Min   float64 `json:"min,omitempty"`
	Max   float64 `json:"max,omitempty"`
	Step  float64 `json:"step,omitempty"`
	// Rate and At make a progress bar, a slider or a clock text run on its
	// own: the value moves on by Rate (in value units a second) from At, in
	// unix milliseconds, the instant Value held. A text with a rate and an
	// instant shows its value, in seconds, as m:ss, up to Max when Max is
	// set; its Text is the fallback of a host that does not know a clock.
	Rate float64 `json:"rate,omitempty"` // a playing part's speed
	At   int64   `json:"at,omitempty"`   // and when it started, in unix milliseconds
	// Marker is a tick across a progress bar at a fraction of its length,
	// from 0 to 1, such as where the usage of a window should be by now.
	Marker *float64 `json:"marker,omitempty"`

	Grow    float64 `json:"grow,omitempty"`
	Gap     int     `json:"gap,omitempty"`
	Padding int     `json:"padding,omitempty"`
	Align   string  `json:"align,omitempty"`

	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`

	// Variant is a list's look: "" for rows, or "grid".
	Variant string `json:"variant,omitempty"`

	// Surface draws a row or column as a card; its Radius, 10 when unset,
	// rounds the card's corners.
	Surface string `json:"surface,omitempty"`
	// Segments are the slices of a donut. Its Width is its diameter, Text
	// the words in its middle and Detail the caption under them. A
	// segmented control takes its choices from Items, the chosen one Active.
	Segments []Segment `json:"segments,omitempty"`
}

// Segment is one slice of a donut part: a share of the whole by its Value,
// in the color role Color.
type Segment struct {
	ID    string  `json:"id"`
	Label string  `json:"label,omitempty"`
	Value float64 `json:"value"`
	Color string  `json:"color,omitempty"`
}

// Item is one row of a list part.
type Item struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Trailing string `json:"trailing,omitempty"`
	Image    string `json:"image,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	// Leading is a short text before the picture or the title, such as a
	// track's number, in a column of its own width.
	Leading string `json:"leading,omitempty"`
	// Active marks the row that is on, such as the track that plays: the
	// app draws it in the accent color, with a speaker in place of the
	// Leading text.
	Active bool `json:"active,omitempty"`
}

// Tree is what a plugin draws in a pane.
type Tree struct {
	Root *Node `json:"root"`
	// Scroll makes the whole pane one scroll: the root takes the height
	// its parts need, and the lists in it show all their rows, instead of
	// each list scrolling on its own under a fixed head.
	Scroll bool `json:"scroll,omitempty"`
}

// ValidateTree checks a tree before the app draws it: known types,
// unique ids, the roles and icons this version knows, and the limits.
func (t *Tree) ValidateTree() error {
	if t == nil || t.Root == nil {
		return fmt.Errorf("plugin tree: a root is required")
	}
	ids := map[string]bool{}
	count := 0
	var walk func(n *Node, depth int) error
	walk = func(n *Node, depth int) error {
		if n == nil {
			return fmt.Errorf("plugin tree: a nil node")
		}
		if depth > MaxDepth {
			return fmt.Errorf("plugin tree: deeper than %d", MaxDepth)
		}
		count++
		if count > MaxNodes {
			return fmt.Errorf("plugin tree: more than %d nodes", MaxNodes)
		}
		if !slices.Contains(KnownNodeTypes, n.Type) {
			return fmt.Errorf("plugin tree: node %q has unknown type %q", n.ID, n.Type)
		}
		if !nodeRe.MatchString(n.ID) {
			return fmt.Errorf("plugin tree: node id %q must match %s", n.ID, nodeRe)
		}
		if ids[n.ID] {
			return fmt.Errorf("plugin tree: node id %q is repeated", n.ID)
		}
		ids[n.ID] = true
		if err := n.validateFields(); err != nil {
			return err
		}
		if len(n.Children) > 0 && n.Type != NodeRow && n.Type != NodeColumn {
			return fmt.Errorf("plugin tree: node %q of type %q holds children", n.ID, n.Type)
		}
		for _, child := range n.Children {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(t.Root, 1)
}

func (n *Node) validateFields() error {
	if n.Gap < 0 || n.Padding < 0 {
		return fmt.Errorf("plugin tree: node %q has a negative gap or padding", n.ID)
	}
	if n.Align != "" && !slices.Contains(KnownAligns, n.Align) {
		return fmt.Errorf("plugin tree: node %q has unknown align %q", n.ID, n.Align)
	}
	if n.Surface != "" && n.Type != NodeRow && n.Type != NodeColumn {
		return fmt.Errorf("plugin tree: node %q of type %q has a surface", n.ID, n.Type)
	}
	if n.Marker != nil && n.Type != NodeProgress {
		return fmt.Errorf("plugin tree: node %q of type %q has a marker", n.ID, n.Type)
	}
	if n.Mark != "" && n.Type != NodeText {
		return fmt.Errorf("plugin tree: node %q of type %q has a mark", n.ID, n.Type)
	}
	if len(n.Segments) > 0 && n.Type != NodeDonut {
		return fmt.Errorf("plugin tree: node %q of type %q has segments", n.ID, n.Type)
	}
	switch n.Type {
	case NodeRow, NodeColumn:
		if n.Text != "" || len(n.Items) > 0 {
			return fmt.Errorf("plugin tree: node %q of type %q holds text or items", n.ID, n.Type)
		}
		if n.Surface != "" && !slices.Contains(KnownSurfaces, n.Surface) {
			return fmt.Errorf("plugin tree: node %q has unknown surface %q", n.ID, n.Surface)
		}
		if n.Radius < 0 {
			return fmt.Errorf("plugin tree: node %q has a negative radius", n.ID)
		}
	case NodeText:
		if n.Role != "" && !slices.Contains(KnownRoles, n.Role) {
			return fmt.Errorf("plugin tree: node %q has unknown role %q", n.ID, n.Role)
		}
		if n.Color != "" && !slices.Contains(KnownColorRoles, n.Color) {
			return fmt.Errorf("plugin tree: node %q has unknown color %q", n.ID, n.Color)
		}
		if n.Icon != "" && !slices.Contains(KnownIcons, n.Icon) {
			return fmt.Errorf("plugin tree: text %q has unknown icon %q", n.ID, n.Icon)
		}
		if n.Mark != "" && !slices.Contains(KnownMarks, n.Mark) {
			return fmt.Errorf("plugin tree: text %q has unknown mark %q", n.ID, n.Mark)
		}
		if n.Lines < 0 {
			return fmt.Errorf("plugin tree: node %q has a negative line limit", n.ID)
		}
		if n.Rate < 0 || n.Value < 0 || n.Max < 0 {
			return fmt.Errorf("plugin tree: text %q has a negative clock value, max or rate", n.ID)
		}
	case NodeImage:
		if n.URL == "" && n.Data == "" {
			return fmt.Errorf("plugin tree: image %q has no url or data", n.ID)
		}
		if n.Width < 0 || n.Height < 0 || n.Radius < 0 {
			return fmt.Errorf("plugin tree: image %q has a negative size or radius", n.ID)
		}
	case NodeButton:
		if n.Label == "" {
			return fmt.Errorf("plugin tree: button %q has no label", n.ID)
		}
	case NodeIconButton:
		if !slices.Contains(KnownIcons, n.Icon) {
			return fmt.Errorf("plugin tree: iconButton %q has unknown icon %q", n.ID, n.Icon)
		}
		if n.Width < 0 || n.Width > 96 {
			return fmt.Errorf("plugin tree: iconButton %q has a size outside 0 to 96", n.ID)
		}
	case NodeSlider:
		if n.Min >= n.Max {
			return fmt.Errorf("plugin tree: slider %q needs min below max", n.ID)
		}
		if n.Rate < 0 {
			return fmt.Errorf("plugin tree: slider %q has a negative rate", n.ID)
		}
	case NodeProgress:
		if n.Rate < 0 {
			return fmt.Errorf("plugin tree: progress %q has a negative rate", n.ID)
		}
		if n.Value < 0 || n.Value > 1 {
			return fmt.Errorf("plugin tree: progress %q is outside 0 to 1", n.ID)
		}
		if n.Color != "" && !slices.Contains(KnownColorRoles, n.Color) {
			return fmt.Errorf("plugin tree: progress %q has unknown color %q", n.ID, n.Color)
		}
		if n.Marker != nil && !(*n.Marker >= 0 && *n.Marker <= 1) {
			return fmt.Errorf("plugin tree: progress %q has a marker outside 0 to 1", n.ID)
		}
	case NodeList:
		if n.Variant != "" && !slices.Contains(KnownVariants, n.Variant) {
			return fmt.Errorf("plugin tree: list %q has unknown variant %q", n.ID, n.Variant)
		}
		if n.Width < 0 || n.Width > 1000 {
			return fmt.Errorf("plugin tree: list %q has a tile width outside 0 to 1000", n.ID)
		}
		itemIDs := map[string]bool{}
		for _, item := range n.Items {
			if !nodeRe.MatchString(item.ID) {
				return fmt.Errorf("plugin tree: list %q has item id %q that must match %s", n.ID, item.ID, nodeRe)
			}
			if itemIDs[item.ID] {
				return fmt.Errorf("plugin tree: list %q repeats item id %q", n.ID, item.ID)
			}
			itemIDs[item.ID] = true
			if item.Title == "" {
				return fmt.Errorf("plugin tree: list %q has item %q without a title", n.ID, item.ID)
			}
		}
	case NodeSearchField:
		if n.Placeholder == "" {
			return fmt.Errorf("plugin tree: searchField %q has no placeholder", n.ID)
		}
	case NodeEmptyState:
		if n.Title == "" {
			return fmt.Errorf("plugin tree: emptyState %q has no title", n.ID)
		}
	case NodeSpacer:
		if n.Grow < 0 {
			return fmt.Errorf("plugin tree: spacer %q has a negative grow", n.ID)
		}
	case NodeDivider:
	case NodeSegmented:
		return n.validateSegmented()
	case NodeDonut:
		return n.validateDonut()
	}
	return nil
}

// validateSegmented checks a segmented control's choices: at least two,
// with ids of their own and titles, and at most one chosen.
func (n *Node) validateSegmented() error {
	if len(n.Items) < 2 {
		return fmt.Errorf("plugin tree: segmented %q needs at least two items", n.ID)
	}
	if len(n.Items) > MaxSegmentedItems {
		return fmt.Errorf("plugin tree: segmented %q has more than %d items", n.ID, MaxSegmentedItems)
	}
	itemIDs := map[string]bool{}
	active := 0
	for _, item := range n.Items {
		if !nodeRe.MatchString(item.ID) {
			return fmt.Errorf("plugin tree: segmented %q has item id %q that must match %s", n.ID, item.ID, nodeRe)
		}
		if itemIDs[item.ID] {
			return fmt.Errorf("plugin tree: segmented %q repeats item id %q", n.ID, item.ID)
		}
		itemIDs[item.ID] = true
		if item.Title == "" {
			return fmt.Errorf("plugin tree: segmented %q has item %q without a title", n.ID, item.ID)
		}
		if item.Active {
			active++
		}
	}
	if active > 1 {
		return fmt.Errorf("plugin tree: segmented %q has %d active items, at most one", n.ID, active)
	}
	return nil
}

// validateDonut checks a donut's diameter and slices.
func (n *Node) validateDonut() error {
	if n.Width != 0 && (n.Width < MinDonut || n.Width > MaxDonut) {
		return fmt.Errorf("plugin tree: donut %q has a diameter outside %d to %d", n.ID, MinDonut, MaxDonut)
	}
	if len(n.Segments) > MaxSegments {
		return fmt.Errorf("plugin tree: donut %q has more than %d segments", n.ID, MaxSegments)
	}
	segIDs := map[string]bool{}
	for _, seg := range n.Segments {
		if !nodeRe.MatchString(seg.ID) {
			return fmt.Errorf("plugin tree: donut %q has segment id %q that must match %s", n.ID, seg.ID, nodeRe)
		}
		if segIDs[seg.ID] {
			return fmt.Errorf("plugin tree: donut %q repeats segment id %q", n.ID, seg.ID)
		}
		segIDs[seg.ID] = true
		if !(seg.Value >= 0) || math.IsInf(seg.Value, 0) {
			return fmt.Errorf("plugin tree: donut %q has segment %q with a negative or invalid value", n.ID, seg.ID)
		}
		if seg.Color != "" && !slices.Contains(KnownColorRoles, seg.Color) {
			return fmt.Errorf("plugin tree: donut %q has segment %q with unknown color %q", n.ID, seg.ID, seg.Color)
		}
	}
	return nil
}

// FormatClock writes a time in seconds as m:ss, or h:mm:ss from an hour.
// A negative or invalid time is zero; a fraction of a second is dropped.
func FormatClock(seconds float64) string {
	if !(seconds > 0) {
		seconds = 0
	}
	total := int64(seconds)
	h, m, s := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", total/60, s)
}

// AdvanceValue is a value moved on by rate, in value units a second, from
// the instant at, in unix milliseconds, to now, kept between lo and hi.
// Without a rate or an instant it is the value, clamped.
func AdvanceValue(value, rate float64, at, now int64, lo, hi float64) float64 {
	if rate > 0 && at > 0 {
		value += rate * float64(now-at) / 1000
	}
	return min(max(value, lo), hi)
}
