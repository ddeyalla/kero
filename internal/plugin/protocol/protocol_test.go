package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	cases := []struct {
		name   string
		method string
		params any
	}{
		{"hello", MethodHello, HelloParams{Protocol: Version, Host: "Shibu Code 2.0.7"}},
		{"open", MethodOpen, OpenParams{Pane: "p1", Widget: "player", State: json.RawMessage(`{"tab":"search"}`), Size: Size{Width: 320, Height: 200}}},
		{"close", MethodClose, CloseParams{Pane: "p1"}},
		{"resize", MethodResize, ResizeParams{Pane: "p1", Size: Size{Width: 400, Height: 300}}},
		{"visible", MethodVisible, VisibleParams{Pane: "p1", Visible: true}},
		{"theme", MethodTheme, ThemeParams{Dark: true, Accent: "#7f5af0"}},
		{"settings", MethodSettings, SettingsParams{Values: map[string]string{"clientId": "abc"}}},
		{"event", MethodEvent, EventParams{Pane: "p1", Node: "play", Kind: KindPress, Value: 0.5}},
		{"command", MethodCommand, CommandParams{ID: "next"}},
		{"render", MethodRender, RenderParams{Pane: "p1", Tree: Tree{Root: &Node{Type: NodeText, ID: "t", Text: "hi"}}}},
		{"state.save", MethodStateSave, StateSaveParams{Pane: "p1", State: json.RawMessage(`{"a":1}`)}},
		{"secret.get", MethodSecretGet, SecretGetParams{Key: "token"}},
		{"secret.set", MethodSecretSet, SecretSetParams{Key: "token", Value: "v"}},
		{"notify", MethodNotify, NotifyParams{Title: "Saved", Body: "Done"}},
		{"openURL", MethodOpenURL, OpenURLParams{URL: "https://example.com"}},
		{"clipboard.write", MethodClipboard, ClipboardWriteParams{Text: "ENG-1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf.Reset()
			req, err := NewRequest(7, c.method, c.params)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if err := enc.Encode(req); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			var got Request
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.Method != c.method {
				t.Fatalf("method = %q, want %q", got.Method, c.method)
			}
			if got.IsNotification() {
				t.Fatal("a request with an id is not a notification")
			}
			want, err := json.Marshal(c.params)
			if err != nil {
				t.Fatalf("marshal want: %v", err)
			}
			if !jsonEqual(want, got.Params) {
				t.Fatalf("params = %s, want %s", got.Params, want)
			}
		})
	}
}

func TestNotificationAndResponseRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	n, err := NewNotification(MethodRender, RenderParams{Pane: "p1"})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if err := enc.Encode(n); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got Request
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.IsNotification() {
		t.Fatal("a notification has no id")
	}

	resp, err := NewResult(json.RawMessage(`7`), HelloResult{Protocol: Version})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if err := enc.Encode(resp); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var gotResp Response
	if err := dec.Decode(&gotResp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if gotResp.Error != nil {
		t.Fatalf("error = %v", gotResp.Error)
	}
	var hello HelloResult
	if err := json.Unmarshal(gotResp.Result, &hello); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if hello.Protocol != Version {
		t.Fatalf("protocol = %d, want %d", hello.Protocol, Version)
	}

	fail := NewFailure(json.RawMessage(`7`), CodeInvalidParams, "no pane")
	if fail.Error == nil || fail.Error.Error() != "no pane" {
		t.Fatalf("failure = %+v", fail)
	}
}

func TestVersion(t *testing.T) {
	if err := CheckVersion(Version); err != nil {
		t.Fatalf("CheckVersion(%d): %v", Version, err)
	}
	if err := CheckVersion(Version + 1); !errors.Is(err, ErrVersion) {
		t.Fatalf("CheckVersion: err = %v, want ErrVersion", err)
	}
}

func TestDecoderRejectsLongLine(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(strings.Repeat("x", MaxLine+10))
	buf.WriteByte('\n')
	enc := NewEncoder(&buf)
	req, err := NewRequest(1, MethodClose, CloseParams{Pane: "p1"})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := enc.Encode(req); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	dec := NewDecoder(&buf)
	var got Request
	if err := dec.Decode(&got); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("Decode long line: err = %v, want ErrLineTooLong", err)
	}
	// The rest of the long line is dropped, so the next one decodes.
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode after long line: %v", err)
	}
	if got.Method != MethodClose {
		t.Fatalf("method = %q, want %q", got.Method, MethodClose)
	}
}

func TestEncoderRefusesLongLine(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(map[string]string{"big": strings.Repeat("x", MaxLine)}); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("Encode: err = %v, want ErrLineTooLong", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote %d bytes of a refused line", buf.Len())
	}
}

func TestManifestValidate(t *testing.T) {
	good := Manifest{
		ID:          "spotify",
		Name:        "Spotify",
		Version:     "0.1.0",
		Run:         []string{"kero", "plugin-run", "spotify"},
		Widgets:     []Widget{{Name: "player", Title: "Spotify", MinWidth: 280, MinHeight: 150}},
		Commands:    []Command{{ID: "play-pause", Title: "Play or pause"}},
		Permissions: Permissions{PermSecrets, PermOpenURL},
		Settings:    []Setting{{Key: "clientId", Label: "Spotify Client ID", Kind: SettingText, Link: "https://developer.spotify.com/dashboard"}},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	withAction := good
	withAction.Settings = append(append([]Setting(nil), good.Settings...), Setting{Key: "play-pause", Label: "Play", Kind: SettingAction})
	if err := withAction.Validate(); err != nil {
		t.Fatalf("an action of a declared command: %v", err)
	}
	if good.WidgetByName("player") == nil {
		t.Fatal("WidgetByName(player) = nil")
	}
	withIcon := good
	withIcon.Widgets = []Widget{{Name: "board", Title: "Board", Icon: "board"}}
	withIcon.Permissions = Permissions{PermFocus, PermOpenURL}
	if err := withIcon.Validate(); err != nil {
		t.Fatalf("a widget icon and the focus permission: %v", err)
	}
	if !good.Permissions.Has(PermSecrets) || good.Permissions.Has(PermNotify) {
		t.Fatal("Permissions.Has is wrong")
	}

	bad := []struct {
		name string
		edit func(*Manifest)
	}{
		{"short id", func(m *Manifest) { m.ID = "x" }},
		{"upper id", func(m *Manifest) { m.ID = "Spotify" }},
		{"empty name", func(m *Manifest) { m.Name = " " }},
		{"no version", func(m *Manifest) { m.Version = "" }},
		{"no run", func(m *Manifest) { m.Run = nil }},
		{"no widgets", func(m *Manifest) { m.Widgets = nil }},
		{"repeated widget", func(m *Manifest) { m.Widgets = append(m.Widgets, m.Widgets[0]) }},
		{"widget without title", func(m *Manifest) { m.Widgets[0].Title = "" }},
		{"repeated command", func(m *Manifest) { m.Commands = append(m.Commands, m.Commands[0]) }},
		{"unknown widget icon", func(m *Manifest) { m.Widgets[0].Icon = "rocket" }},
		{"unknown permission", func(m *Manifest) { m.Permissions = Permissions{"network"} }},
		{"unknown setting kind", func(m *Manifest) { m.Settings[0].Kind = "number" }},
		{"repeated setting", func(m *Manifest) { m.Settings = append(m.Settings, m.Settings[0]) }},
		{"action without a command", func(m *Manifest) {
			m.Settings = append(m.Settings, Setting{Key: "nope", Label: "Nope", Kind: SettingAction})
		}},
		{"secret action", func(m *Manifest) {
			m.Settings = append(m.Settings, Setting{Key: "play-pause", Label: "Play", Kind: SettingAction, Secret: true})
		}},
		{"linked action", func(m *Manifest) {
			m.Settings = append(m.Settings, Setting{Key: "play-pause", Label: "Play", Kind: SettingAction, Link: "https://example.com"})
		}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			m := good
			m.Widgets = append([]Widget(nil), good.Widgets...)
			m.Commands = append([]Command(nil), good.Commands...)
			m.Settings = append([]Setting(nil), good.Settings...)
			c.edit(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("Validate = nil, want an error")
			}
		})
	}
}

func TestValidateTree(t *testing.T) {
	good := Tree{Root: &Node{
		Type: NodeColumn, ID: "root", Padding: 12, Gap: 8,
		Children: []*Node{
			{Type: NodeText, ID: "title", Text: "Hello", Role: RoleTitle, Color: ColorText, Marquee: true},
			{Type: NodeImage, ID: "art", URL: "https://example.com/a.png", Width: 40, Height: 40, Radius: 6, Tint: true},
			{Type: NodeRow, ID: "controls", Align: AlignCenter, Children: []*Node{
				{Type: NodeIconButton, ID: "prev", Icon: "previous"},
				{Type: NodeIconButton, ID: "play", Icon: "play", Primary: true},
				{Type: NodeIconButton, ID: "next", Icon: "next"},
			}},
			{Type: NodeProgress, ID: "bar", Value: 0.4, Rate: 1, At: 1700000000000},
			{Type: NodeList, ID: "results", Items: []Item{{ID: "r1", Title: "One"}, {ID: "r2", Title: "Two", Subtitle: "Artist"}}},
			{Type: NodeSearchField, ID: "search", Placeholder: "Search Spotify"},
			{Type: NodeEmptyState, ID: "empty", Title: "Nothing playing", Detail: "Open Spotify", Action: "Open"},
			{Type: NodeSpacer, ID: "gap", Grow: 1},
			{Type: NodeDivider, ID: "line"},
			{Type: NodeButton, ID: "signin", Label: "Sign In", Primary: true},
			{Type: NodeSlider, ID: "seek", Value: 0.2, Min: 0, Max: 1},
		},
	}}
	if err := good.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree: %v", err)
	}

	bad := []struct {
		name string
		tree Tree
	}{
		{"no root", Tree{}},
		{"unknown type", Tree{Root: &Node{Type: "webview", ID: "w"}}},
		{"bad id", Tree{Root: &Node{Type: NodeSpacer, ID: "has space"}}},
		{"repeated id", Tree{Root: &Node{Type: NodeColumn, ID: "c", Children: []*Node{
			{Type: NodeText, ID: "t", Text: "a"}, {Type: NodeText, ID: "t", Text: "b"},
		}}}},
		{"unknown role", Tree{Root: &Node{Type: NodeText, ID: "t", Role: "huge"}}},
		{"unknown color", Tree{Root: &Node{Type: NodeText, ID: "t", Color: "#ff0000"}}},
		{"children on text", Tree{Root: &Node{Type: NodeText, ID: "t", Text: "a", Children: []*Node{
			{Type: NodeText, ID: "u", Text: "b"},
		}}}},
		{"image without source", Tree{Root: &Node{Type: NodeImage, ID: "i"}}},
		{"unknown icon", Tree{Root: &Node{Type: NodeIconButton, ID: "b", Icon: "rocket"}}},
		{"button without label", Tree{Root: &Node{Type: NodeButton, ID: "b"}}},
		{"empty state without title", Tree{Root: &Node{Type: NodeEmptyState, ID: "e"}}},
		{"slider range", Tree{Root: &Node{Type: NodeSlider, ID: "s", Min: 2, Max: 1}}},
		{"progress range", Tree{Root: &Node{Type: NodeProgress, ID: "p", Value: 2}}},
		{"repeated item", Tree{Root: &Node{Type: NodeList, ID: "l", Items: []Item{
			{ID: "a", Title: "A"}, {ID: "a", Title: "B"},
		}}}},
		{"item without title", Tree{Root: &Node{Type: NodeList, ID: "l", Items: []Item{{ID: "a"}}}}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if err := c.tree.ValidateTree(); err == nil {
				t.Fatal("ValidateTree = nil, want an error")
			}
		})
	}
}

func TestValidateTreeLimits(t *testing.T) {
	deep := &Node{Type: NodeColumn, ID: "n0"}
	cur := deep
	for i := 1; i <= MaxDepth; i++ {
		next := &Node{Type: NodeColumn, ID: "n" + itoa(i)}
		cur.Children = []*Node{next}
		cur = next
	}
	if err := (&Tree{Root: deep}).ValidateTree(); err == nil {
		t.Fatal("a tree deeper than MaxDepth passed")
	}

	many := &Node{Type: NodeColumn, ID: "root"}
	for i := 0; i < MaxNodes; i++ {
		many.Children = append(many.Children, &Node{Type: NodeSpacer, ID: "s" + itoa(i)})
	}
	if err := (&Tree{Root: many}).ValidateTree(); err == nil {
		t.Fatal("a tree larger than MaxNodes passed")
	}
}

func TestTargetRoundTrip(t *testing.T) {
	in := Target{Plugin: "spotify", Widget: "player", State: json.RawMessage(`{"tab":"albums"}`)}
	s, err := EncodeTarget(in)
	if err != nil {
		t.Fatalf("EncodeTarget: %v", err)
	}
	out, err := DecodeTarget(s)
	if err != nil {
		t.Fatalf("DecodeTarget: %v", err)
	}
	if out.Plugin != in.Plugin || out.Widget != in.Widget || !jsonEqual(out.State, in.State) {
		t.Fatalf("decoded %+v, want %+v", out, in)
	}

	if _, err := EncodeTarget(Target{Plugin: "Bad Id"}); !errors.Is(err, ErrTarget) {
		t.Fatalf("EncodeTarget bad id: err = %v, want ErrTarget", err)
	}
	if _, err := EncodeTarget(Target{Plugin: "spotify", State: json.RawMessage("{")}); !errors.Is(err, ErrTarget) {
		t.Fatalf("EncodeTarget bad state: err = %v, want ErrTarget", err)
	}
	for _, s := range []string{"", "{"} {
		if _, err := DecodeTarget(s); !errors.Is(err, ErrTarget) {
			t.Fatalf("DecodeTarget(%q): err = %v, want ErrTarget", s, err)
		}
	}
	if _, err := DecodeTarget(`{"plugin":"UPPER"}`); !errors.Is(err, ErrTarget) {
		t.Fatalf("DecodeTarget upper: err = %v, want ErrTarget", err)
	}
}

func TestMethodPermission(t *testing.T) {
	p := Permissions{PermSecrets}
	if !p.Allows(MethodRender) || !p.Allows(MethodSecretGet) {
		t.Fatal("a plugin with secrets may render and read secrets")
	}
	if p.Allows(MethodNotify) || p.Allows(MethodOpenURL) {
		t.Fatal("a plugin without notify or openURL may not call them")
	}
	if p.Allows("secret.steal") {
		t.Fatal("an unknown method is refused")
	}
	for _, method := range []string{MethodRender, MethodStateSave, MethodSecretGet, MethodSecretSet, MethodNotify, MethodOpenURL, MethodClipboard} {
		if _, ok := MethodPermission[method]; !ok {
			t.Fatalf("no permission listed for %q", method)
		}
	}
}

func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	aj, err := json.Marshal(av)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(bv)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func TestClipboardNeedsItsPermission(t *testing.T) {
	if (Permissions{PermOpenURL, PermNotify}).Allows(MethodClipboard) {
		t.Fatal("a plugin without the clipboard permission may not copy")
	}
	if !(Permissions{PermClipboard}).Allows(MethodClipboard) {
		t.Fatal("a plugin with the clipboard permission may copy")
	}
	m := Manifest{ID: "linear", Name: "Linear", Version: "1", Run: []string{"x"},
		Widgets: []Widget{{Name: "main", Title: "Main"}}, Permissions: Permissions{PermClipboard}}
	if err := m.Validate(); err != nil {
		t.Fatalf("the clipboard permission is known: %v", err)
	}
}
