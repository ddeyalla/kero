package sdk

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
)

// host is the app side of a plugin under test: it writes the calls a
// host makes and reads the calls the plugin makes back.
type host struct {
	enc      *protocol.Encoder
	dec      *protocol.Decoder
	in       io.WriteCloser
	out      io.WriteCloser
	pluginIn io.Reader
}

func newHost() *host {
	toPluginR, toPluginW := io.Pipe()
	toHostR, toHostW := io.Pipe()
	h := &host{
		enc: protocol.NewEncoder(toPluginW),
		dec: protocol.NewDecoder(toHostR),
		in:  toPluginW,
		out: toHostW,
	}
	h.pluginIn = toPluginR
	return h
}

func (h *host) send(t *testing.T, v any) {
	t.Helper()
	if err := h.enc.Encode(v); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (h *host) request(t *testing.T, timeout time.Duration) *protocol.Request {
	t.Helper()
	type read struct {
		req *protocol.Request
		err error
	}
	ch := make(chan read, 1)
	go func() {
		var req protocol.Request
		err := h.dec.Decode(&req)
		ch <- read{&req, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read: %v", r.err)
		}
		return r.req
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the plugin")
		return nil
	}
}

func (h *host) respond(t *testing.T, id json.RawMessage, result any) {
	t.Helper()
	resp, err := protocol.NewResult(id, result)
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	h.send(t, resp)
}

func (h *host) fail(t *testing.T, id json.RawMessage, code int, message string) {
	t.Helper()
	h.send(t, protocol.NewFailure(id, code, message))
}

func (h *host) hello(t *testing.T, version int) *protocol.Response {
	t.Helper()
	req, err := protocol.NewRequest(1, protocol.MethodHello, protocol.HelloParams{Protocol: version, Host: "test"})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	h.send(t, req)
	var resp protocol.Response
	if err := h.dec.Decode(&resp); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	return &resp
}

func (h *host) close() {
	h.in.Close()
	h.out.Close()
}

func runPlugin(t *testing.T, h *host, handlers Handlers) <-chan error {
	t.Helper()
	errs := make(chan error, 1)
	go func() { errs <- RunIO(h.pluginIn, h.out, handlers) }()
	return errs
}

func waitEnd(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("RunIO: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunIO did not end")
	}
}

func TestRunHandshakeAndRender(t *testing.T) {
	h := newHost()
	defer h.close()
	rendered := make(chan error, 1)
	errs := runPlugin(t, h, Handlers{
		Open: func(c *Conn, e OpenEvent) {
			if e.Pane != "p1" || e.Widget != "player" || e.Size.Width != 300 {
				t.Errorf("open = %+v", e)
			}
			rendered <- c.Render(e.Pane, Text("hello", "Hi").Role(protocol.RoleTitle).Tree())
		},
	})

	hello := h.hello(t, protocol.Version)
	if hello.Error != nil {
		t.Fatalf("hello: %v", hello.Error)
	}
	var result protocol.HelloResult
	if err := json.Unmarshal(hello.Result, &result); err != nil {
		t.Fatalf("hello result: %v", err)
	}
	if result.Protocol != protocol.Version {
		t.Fatalf("protocol = %d, want %d", result.Protocol, protocol.Version)
	}

	open, err := protocol.NewNotification(protocol.MethodOpen, protocol.OpenParams{
		Pane: "p1", Widget: "player", Size: protocol.Size{Width: 300, Height: 160},
	})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	h.send(t, open)

	req := h.request(t, 5*time.Second)
	if req.Method != protocol.MethodRender {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodRender)
	}
	var params protocol.RenderParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("render params: %v", err)
	}
	if params.Pane != "p1" || params.Tree.Root == nil || params.Tree.Root.Text != "Hi" {
		t.Fatalf("render = %+v", params)
	}
	tree := params.Tree
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("rendered tree: %v", err)
	}
	h.respond(t, req.ID, struct{}{})
	if err := <-rendered; err != nil {
		t.Fatalf("Render: %v", err)
	}

	h.in.Close()
	waitEnd(t, errs)
}

func TestPluginCallsHost(t *testing.T) {
	h := newHost()
	defer h.close()
	errs := runPlugin(t, h, Handlers{
		Command: func(c *Conn, id string) {
			if id != "save" {
				t.Errorf("command = %q", id)
			}
			if err := c.SaveState("p1", map[string]string{"tab": "albums"}); err != nil {
				t.Errorf("SaveState: %v", err)
			}
			if err := c.Notify("Saved", "Your albums"); err != nil {
				t.Errorf("Notify: %v", err)
			}
			if err := c.OpenURL("https://example.com"); err != nil {
				t.Errorf("OpenURL: %v", err)
			}
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}

	cmd, err := protocol.NewNotification(protocol.MethodCommand, protocol.CommandParams{ID: "save"})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	h.send(t, cmd)

	req := h.request(t, 5*time.Second)
	if req.Method != protocol.MethodStateSave {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodStateSave)
	}
	var saved protocol.StateSaveParams
	if err := json.Unmarshal(req.Params, &saved); err != nil {
		t.Fatalf("state.save params: %v", err)
	}
	if saved.Pane != "p1" || string(saved.State) != `{"tab":"albums"}` {
		t.Fatalf("state.save = %+v", saved)
	}
	h.respond(t, req.ID, struct{}{})

	req = h.request(t, 5*time.Second)
	if req.Method != protocol.MethodNotify {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodNotify)
	}
	h.respond(t, req.ID, struct{}{})

	req = h.request(t, 5*time.Second)
	if req.Method != protocol.MethodOpenURL {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodOpenURL)
	}
	var opened protocol.OpenURLParams
	if err := json.Unmarshal(req.Params, &opened); err != nil {
		t.Fatalf("openURL params: %v", err)
	}
	if opened.URL != "https://example.com" {
		t.Fatalf("openURL = %q", opened.URL)
	}
	h.respond(t, req.ID, struct{}{})

	h.in.Close()
	waitEnd(t, errs)
}

func TestSecrets(t *testing.T) {
	h := newHost()
	defer h.close()
	got := make(chan string, 1)
	errs := runPlugin(t, h, Handlers{
		Command: func(c *Conn, _ string) {
			value, err := c.Secret("token")
			if err != nil {
				t.Errorf("Secret: %v", err)
				got <- ""
				return
			}
			got <- value
			if err := c.SetSecret("token", "new"); err != nil {
				t.Errorf("SetSecret: %v", err)
			}
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}
	cmd, err := protocol.NewNotification(protocol.MethodCommand, protocol.CommandParams{ID: "read"})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	h.send(t, cmd)

	req := h.request(t, 5*time.Second)
	if req.Method != protocol.MethodSecretGet {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodSecretGet)
	}
	var ask protocol.SecretGetParams
	if err := json.Unmarshal(req.Params, &ask); err != nil {
		t.Fatalf("secret.get params: %v", err)
	}
	if ask.Key != "token" {
		t.Fatalf("key = %q", ask.Key)
	}
	h.respond(t, req.ID, protocol.SecretGetResult{Value: "s3cret"})
	if value := <-got; value != "s3cret" {
		t.Fatalf("value = %q", value)
	}

	req = h.request(t, 5*time.Second)
	if req.Method != protocol.MethodSecretSet {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodSecretSet)
	}
	var set protocol.SecretSetParams
	if err := json.Unmarshal(req.Params, &set); err != nil {
		t.Fatalf("secret.set params: %v", err)
	}
	if set.Key != "token" || set.Value != "new" {
		t.Fatalf("secret.set = %+v", set)
	}
	h.respond(t, req.ID, struct{}{})

	h.in.Close()
	waitEnd(t, errs)
}

func TestRenderFailureReachesTheHandler(t *testing.T) {
	h := newHost()
	defer h.close()
	failure := make(chan error, 1)
	errs := runPlugin(t, h, Handlers{
		Open: func(c *Conn, e OpenEvent) {
			failure <- c.Render(e.Pane, Text("t", "hi").Tree())
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}
	open, err := protocol.NewNotification(protocol.MethodOpen, protocol.OpenParams{Pane: "p1", Widget: "player"})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	h.send(t, open)

	req := h.request(t, 5*time.Second)
	h.fail(t, req.ID, protocol.CodeInvalidParams, "not allowed")
	if err := <-failure; err == nil {
		t.Fatal("Render = nil, want the host's error")
	}

	h.in.Close()
	waitEnd(t, errs)
}

// A handler that waits for the answer to a render, while the host sends
// more calls than any buffer holds, still gets its answer: the read loop
// never waits for the handlers, and the calls run after, in their order.
func TestAWaitingHandlerDoesNotStopTheReadLoop(t *testing.T) {
	h := newHost()
	defer h.close()
	rendered := make(chan error, 1)
	widths := make(chan int, 32)
	errs := runPlugin(t, h, Handlers{
		Open: func(c *Conn, e OpenEvent) {
			rendered <- c.Render(e.Pane, Text("t", "hi").Tree())
		},
		Resize: func(c *Conn, pane string, size protocol.Size) {
			widths <- size.Width
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}
	open, err := protocol.NewNotification(protocol.MethodOpen, protocol.OpenParams{Pane: "p1", Widget: "player"})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	h.send(t, open)
	req := h.request(t, 5*time.Second)
	if req.Method != protocol.MethodRender {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodRender)
	}

	// The pipe is synchronous: a plugin that stopped reading would hold
	// these writes forever, so they run off the test's goroutine.
	const resizes = 20
	sent := make(chan error, 1)
	go func() {
		for i := 1; i <= resizes; i++ {
			resize, err := protocol.NewNotification(protocol.MethodResize, protocol.ResizeParams{Pane: "p1", Size: protocol.Size{Width: i, Height: 1}})
			if err == nil {
				err = h.enc.Encode(resize)
			}
			if err != nil {
				sent <- err
				return
			}
		}
		resp, err := protocol.NewResult(req.ID, struct{}{})
		if err == nil {
			err = h.enc.Encode(resp)
		}
		sent <- err
	}()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin stopped reading while a handler waited")
	}
	select {
	case err := <-rendered:
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the render never got its answer")
	}
	for i := 1; i <= resizes; i++ {
		select {
		case w := <-widths:
			if w != i {
				t.Fatalf("resize %d came as width %d: out of order", i, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("resize %d never reached its handler", i)
		}
	}

	h.in.Close()
	waitEnd(t, errs)
}

func TestBadVersionIsRefused(t *testing.T) {
	h := newHost()
	defer h.close()
	errs := runPlugin(t, h, Handlers{})

	resp := h.hello(t, protocol.Version+1)
	if resp.Error == nil {
		t.Fatal("a bad protocol version got a result, want an error")
	}
	select {
	case err := <-errs:
		if !errors.Is(err, protocol.ErrVersion) {
			t.Fatalf("RunIO: %v, want ErrVersion", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunIO did not end after a bad version")
	}
}

func TestPartsBuildValidTree(t *testing.T) {
	root := Column("root",
		Text("title", "Hello").Role(protocol.RoleTitle).Color(protocol.ColorText).Marquee(),
		Image("art", "https://example.com/a.png").Size(40, 40).Radius(6).Tint(),
		Row("controls",
			IconButton("prev", "previous"),
			IconButton("play", "play").Primary(),
			IconButton("next", "next"),
		).Align(protocol.AlignCenter).Gap(4),
		Progress("bar", 0.25).Playing(1, 1700000000000),
		Progress("plain", 0.5),
		Slider("seek", 0.2, 0, 1).Step(0.01),
		List("results", protocol.Item{ID: "r1", Title: "One"}),
		SearchField("search", "Search Spotify").Query("miles"),
		EmptyState("empty", "Nothing playing", "Open Spotify").Action("Open"),
		Spacer("gap"),
		Divider("line"),
		Button("signin", "Sign In").Primary(),
	).Padding(12).Gap(8)
	tree := root.Tree()
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree: %v", err)
	}
	built := root.Build()
	if len(built.Children) != 12 {
		t.Fatalf("children = %d, want 12", len(built.Children))
	}
	if built.Children[0].Role != protocol.RoleTitle || !built.Children[0].Marquee {
		t.Fatalf("title = %+v", built.Children[0])
	}
	if built.Children[1].URL == "" || !built.Children[1].Tint {
		t.Fatalf("art = %+v", built.Children[1])
	}
	if got := built.Children[2].Children[1].Icon; got != "play" {
		t.Fatalf("play icon = %q", got)
	}
	if built.Children[3].Rate != 1 || built.Children[3].At == 0 {
		t.Fatalf("bar = %+v", built.Children[3])
	}
	if got := built.Children[7]; got.Type != protocol.NodeSearchField || got.Text != "miles" {
		t.Fatalf("search = %+v, want the query in its text", got)
	}
}

// A tree equal to the one last sent for a pane is not sent again; a
// different one is, and so is one after a failed render.
func TestRenderSkipsAnUnchangedTree(t *testing.T) {
	h := newHost()
	defer h.close()
	done := make(chan error, 1)
	errs := runPlugin(t, h, Handlers{
		Open: func(c *Conn, e OpenEvent) {
			var err error
			for _, text := range []string{"a", "a", "b", "b", "a"} {
				if err = c.Render(e.Pane, Text("t", text).Tree()); err != nil {
					break
				}
			}
			done <- err
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}
	open, err := protocol.NewNotification(protocol.MethodOpen, protocol.OpenParams{Pane: "p1", Widget: "player"})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	h.send(t, open)

	var texts []string
	for range 3 {
		req := h.request(t, 5*time.Second)
		var params protocol.RenderParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("render params: %v", err)
		}
		texts = append(texts, params.Tree.Root.Text)
		h.respond(t, req.ID, struct{}{})
	}
	if err := <-done; err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := strings.Join(texts, ""); got != "aba" {
		t.Fatalf("sent %q, want aba: the repeats are skipped", got)
	}
	h.in.Close()
	waitEnd(t, errs)
}

// A failed render is not remembered, so the same tree goes out again.
func TestRenderRetriesAfterAFailure(t *testing.T) {
	h := newHost()
	defer h.close()
	done := make(chan [2]error, 1)
	errs := runPlugin(t, h, Handlers{
		Open: func(c *Conn, e OpenEvent) {
			tree := Text("t", "x").Tree()
			done <- [2]error{c.Render(e.Pane, tree), c.Render(e.Pane, tree)}
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}
	open, _ := protocol.NewNotification(protocol.MethodOpen, protocol.OpenParams{Pane: "p1", Widget: "player"})
	h.send(t, open)
	req := h.request(t, 5*time.Second)
	h.fail(t, req.ID, protocol.CodeInvalidParams, "no")
	req = h.request(t, 5*time.Second)
	h.respond(t, req.ID, struct{}{})
	got := <-done
	if got[0] == nil || got[1] != nil {
		t.Fatalf("renders = %v, want a failure then success", got)
	}
	h.in.Close()
	waitEnd(t, errs)
}

// Renders from many goroutines, with opens between them, neither race nor
// grow the record of panes past its bound.
func TestRenderRacesWithForgetAndStaysBounded(t *testing.T) {
	h := newHost()
	defer h.close()
	c := &Conn{
		enc:     protocol.NewEncoder(io.Discard),
		pending: map[string]chan *protocol.Response{},
		done:    make(chan struct{}),
	}
	// Answer every call at once.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 50 {
				pane := "p" + strconv.Itoa((i+j)%3)
				if j%7 == 0 {
					c.forgetRender(pane)
				}
				_ = c.paneRender(pane)
			}
		}()
	}
	wg.Wait()
	for i := range 2 * maxRenderPanes {
		c.paneRender("pane-" + strconv.Itoa(i))
	}
	c.rendersMu.Lock()
	n := len(c.renders)
	c.rendersMu.Unlock()
	if n > maxRenderPanes {
		t.Fatalf("the record holds %d panes, want at most %d", n, maxRenderPanes)
	}
}

// A forget that waits for a render in flight is not undone by its store.
func TestForgetWinsOverARenderInFlight(t *testing.T) {
	h := newHost()
	defer h.close()
	done := make(chan error, 1)
	errs := runPlugin(t, h, Handlers{
		Open: func(c *Conn, e OpenEvent) {
			go func() { done <- c.Render(e.Pane, Text("t", "x").Tree()) }()
		},
		Close: func(c *Conn, pane string) {
			// The close arrives while the render waits for the host.
			c.mu.Lock()
			c.mu.Unlock()
		},
	})
	if resp := h.hello(t, protocol.Version); resp.Error != nil {
		t.Fatalf("hello: %v", resp.Error)
	}
	open, _ := protocol.NewNotification(protocol.MethodOpen, protocol.OpenParams{Pane: "p1", Widget: "w"})
	h.send(t, open)
	req := h.request(t, 5*time.Second)
	closeMsg, _ := protocol.NewNotification(protocol.MethodClose, protocol.CloseParams{Pane: "p1"})
	h.send(t, closeMsg)
	time.Sleep(50 * time.Millisecond)
	h.respond(t, req.ID, struct{}{})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Open again: the same tree must be sent again.
	h.send(t, open)
	req = h.request(t, 5*time.Second)
	h.respond(t, req.ID, struct{}{})
	h.in.Close()
	waitEnd(t, errs)
}

// Active switches an icon button on, and Leading and Active travel on the
// items of a list.
func TestActiveParts(t *testing.T) {
	on := IconButton("shuffle", "shuffle").Active(true).Build()
	off := IconButton("repeat", "repeat").Build()
	if !on.Active || off.Active {
		t.Fatalf("active = %v and %v, want true and false", on.Active, off.Active)
	}
	list := List("tracks", protocol.Item{ID: "t-0", Title: "A", Leading: "1", Active: true}).Build()
	tree := protocol.Tree{Root: Column("root", IconButton("shuffle", "shuffle").Active(true), List("tracks", list.Items...)).Build()}
	if err := tree.ValidateTree(); err != nil {
		t.Fatal(err)
	}
	if item := list.Items[0]; item.Leading != "1" || !item.Active {
		t.Fatalf("item = %+v", item)
	}
}
