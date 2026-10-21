// Package sdk is the plugin side of the Shibu plugin protocol: the
// loop that reads the host's calls, the calls a plugin makes back, and
// the part builders a plugin draws with.
//
// A plugin talks to its host through this package alone. It is internal
// for now; BRU-207 makes it public.
package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"kero/internal/plugin/protocol"
)

// ErrClosed is what a call reads as after the host went away.
var ErrClosed = errors.New("plugin sdk: connection closed")

// OpenEvent is the host's call to draw one of the plugin's panes.
type OpenEvent struct {
	Pane   string
	Widget string
	State  json.RawMessage
	Size   protocol.Size
}

// Handlers are what a plugin answers to the host. Each handler gets the
// connection, so that it can draw or save state while it runs. A nil
// handler is ignored, so that a plugin writes only the calls it needs.
type Handlers struct {
	Open     func(c *Conn, e OpenEvent)
	Close    func(c *Conn, pane string)
	Resize   func(c *Conn, pane string, size protocol.Size)
	Visible  func(c *Conn, pane string, visible bool)
	Theme    func(c *Conn, theme protocol.ThemeParams)
	Settings func(c *Conn, values map[string]string)
	Event    func(c *Conn, event protocol.EventParams)
	Command  func(c *Conn, id string)
}

// Run runs the plugin on its stdin and stdout until the host goes away.
func Run(handlers Handlers) error { return RunIO(os.Stdin, os.Stdout, handlers) }

// RunIO runs the plugin on r and w, which tests use in place of the
// standard streams.
func RunIO(r io.Reader, w io.Writer, handlers Handlers) error {
	c := &Conn{
		enc:      protocol.NewEncoder(w),
		dec:      protocol.NewDecoder(r),
		handlers: handlers,
		pending:  map[string]chan *protocol.Response{},
		done:     make(chan struct{}),
		incoming: make(chan *protocol.Request),
		requests: make(chan *protocol.Request),
	}
	go c.readLoop()
	go c.queue()
	var running sync.WaitGroup
	running.Add(1)
	go func() {
		defer running.Done()
		c.work()
	}()
	<-c.done
	// Wait for the handler loop, so that no handler runs after RunIO
	// returns and reads or logs on a connection that is gone.
	running.Wait()
	if err := c.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Conn is one plugin's connection to its host. The loop reads on its
// own goroutine, so that a handler can call the host and wait for the
// answer.
//
// The read loop never waits on a handler. It hands the host's calls to a
// queue with no bound, and the handlers take them from it in order: a
// handler that waits for the answer to a render, while the host sends more
// calls than any buffer holds, still gets its answer.
type Conn struct {
	enc      *protocol.Encoder
	dec      *protocol.Decoder
	handlers Handlers

	next     atomic.Int64
	mu       sync.Mutex
	pending  map[string]chan *protocol.Response
	incoming chan *protocol.Request // from the read loop to the queue
	requests chan *protocol.Request // from the queue to the handlers

	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

// Err is what ended the connection, nil when the host closed it.
func (c *Conn) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

// Close stops the connection.
func (c *Conn) Close() { c.stop(nil) }

func (c *Conn) stop(err error) {
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		close(c.done)
	})
}

func (c *Conn) readLoop() {
	for {
		line, err := c.dec.ReadLine()
		if err != nil {
			if errors.Is(err, protocol.ErrLineTooLong) {
				// The rest of the line is dropped; the next one is read
				// as usual.
				continue
			}
			c.stop(err)
			return
		}
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *protocol.Error `json:"error"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Method != "" {
			var req protocol.Request
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}
			select {
			case c.incoming <- &req:
			case <-c.done:
				return
			}
			continue
		}
		key := string(probe.ID)
		c.mu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		ch <- &protocol.Response{JSONRPC: "2.0", ID: probe.ID, Result: probe.Result, Error: probe.Error}
	}
}

// queue holds the host's calls between the read loop and the handlers, in
// the order they came. It takes a call whenever one comes, so the read loop
// waits for it only as long as one append takes, never for a handler.
func (c *Conn) queue() {
	var calls []*protocol.Request
	for {
		var out chan *protocol.Request
		var head *protocol.Request
		if len(calls) > 0 {
			out, head = c.requests, calls[0]
		}
		select {
		case req := <-c.incoming:
			calls = append(calls, req)
		case out <- head:
			calls[0] = nil
			calls = calls[1:]
		case <-c.done:
			return
		}
	}
}

func (c *Conn) work() {
	for {
		select {
		case req := <-c.requests:
			c.handle(req)
		case <-c.done:
			return
		}
	}
}

func (c *Conn) handle(req *protocol.Request) {
	switch req.Method {
	case protocol.MethodHello:
		var p protocol.HelloParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "hello without params")
			return
		}
		if err := protocol.CheckVersion(p.Protocol); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, err.Error())
			c.stop(err)
			return
		}
		c.reply(req.ID, protocol.HelloResult{Protocol: protocol.Version})
	case protocol.MethodOpen:
		var p protocol.OpenParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "open without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Open != nil {
			c.handlers.Open(c, OpenEvent{Pane: p.Pane, Widget: p.Widget, State: p.State, Size: p.Size})
		}
	case protocol.MethodClose:
		var p protocol.CloseParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "close without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Close != nil {
			c.handlers.Close(c, p.Pane)
		}
	case protocol.MethodResize:
		var p protocol.ResizeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "resize without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Resize != nil {
			c.handlers.Resize(c, p.Pane, p.Size)
		}
	case protocol.MethodVisible:
		var p protocol.VisibleParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "visible without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Visible != nil {
			c.handlers.Visible(c, p.Pane, p.Visible)
		}
	case protocol.MethodTheme:
		var p protocol.ThemeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "theme without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Theme != nil {
			c.handlers.Theme(c, p)
		}
	case protocol.MethodSettings:
		var p protocol.SettingsParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "settings without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Settings != nil {
			c.handlers.Settings(c, p.Values)
		}
	case protocol.MethodEvent:
		var p protocol.EventParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "event without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Event != nil {
			c.handlers.Event(c, p)
		}
	case protocol.MethodCommand:
		var p protocol.CommandParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			c.fail(req.ID, protocol.CodeInvalidParams, "command without params")
			return
		}
		c.reply(req.ID, struct{}{})
		if c.handlers.Command != nil {
			c.handlers.Command(c, p.ID)
		}
	default:
		c.fail(req.ID, protocol.CodeNoMethod, fmt.Sprintf("unknown method %q", req.Method))
	}
}

func (c *Conn) reply(id json.RawMessage, result any) {
	if len(id) == 0 {
		return
	}
	resp, err := protocol.NewResult(id, result)
	if err != nil {
		c.fail(id, protocol.CodeInternal, err.Error())
		return
	}
	if err := c.enc.Encode(resp); err != nil {
		c.stop(err)
	}
}

func (c *Conn) fail(id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		return
	}
	if err := c.enc.Encode(protocol.NewFailure(id, code, message)); err != nil {
		c.stop(err)
	}
}

func (c *Conn) call(method string, params any, result any) error {
	id := c.next.Add(1)
	req, err := protocol.NewRequest(id, method, params)
	if err != nil {
		return err
	}
	key := strconv.FormatInt(id, 10)
	ch := make(chan *protocol.Response, 1)
	c.mu.Lock()
	c.pending[key] = ch
	c.mu.Unlock()
	if err := c.enc.Encode(req); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return err
	}
	select {
	case resp := <-ch:
		return decodeResult(resp, result)
	case <-c.done:
		// The host can answer in the same moment it closes the
		// connection. A response that is already here wins.
		select {
		case resp := <-ch:
			return decodeResult(resp, result)
		default:
			return ErrClosed
		}
	}
}

// decodeResult turns one JSON-RPC response into the call's result.
func decodeResult(resp *protocol.Response, out any) error {
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Render replaces what one pane draws.
func (c *Conn) Render(pane string, tree protocol.Tree) error {
	return c.call(protocol.MethodRender, protocol.RenderParams{Pane: pane, Tree: tree}, nil)
}

// SaveState keeps a pane's state in its target, so that the pane comes
// back after a restart.
func (c *Conn) SaveState(pane string, state any) error {
	raw, err := marshalState(state)
	if err != nil {
		return err
	}
	return c.call(protocol.MethodStateSave, protocol.StateSaveParams{Pane: pane, State: raw}, nil)
}

// Secret reads one of the plugin's own secrets, "" when it is not set.
func (c *Conn) Secret(key string) (string, error) {
	var out protocol.SecretGetResult
	if err := c.call(protocol.MethodSecretGet, protocol.SecretGetParams{Key: key}, &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// SetSecret keeps one of the plugin's own secrets. An empty value
// removes it.
func (c *Conn) SetSecret(key, value string) error {
	return c.call(protocol.MethodSecretSet, protocol.SecretSetParams{Key: key, Value: value}, nil)
}

// Notify shows the user a notification.
func (c *Conn) Notify(title, body string) error {
	return c.call(protocol.MethodNotify, protocol.NotifyParams{Title: title, Body: body}, nil)
}

// OpenURL opens a link in the browser.
func (c *Conn) OpenURL(url string) error {
	return c.call(protocol.MethodOpenURL, protocol.OpenURLParams{URL: url}, nil)
}

func marshalState(state any) (json.RawMessage, error) {
	if raw, ok := state.(json.RawMessage); ok {
		if len(raw) > 0 && !json.Valid(raw) {
			return nil, fmt.Errorf("plugin sdk: state is not JSON")
		}
		return raw, nil
	}
	b, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return b, nil
}
