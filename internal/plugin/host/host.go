// Package host runs the plugins the app hosts, one process per plugin,
// and carries the messages of their panes between the app and the plugin.
//
// A plugin is a process that speaks the JSON-RPC protocol of
// internal/plugin/protocol on its standard input and output. It is not a
// goroutine and not a library: a plugin that hangs, crashes or leaks can
// not take the app with it.
//
// The app starts a plugin's process on the first pane of that plugin, and
// stops it a few seconds after its last pane closes. One process serves
// every pane of the plugin, so two panes of one plugin share it.
//
// All I/O is off the UI goroutine. The process reads and writes in its own
// goroutines; the app hears of what happened through Callbacks, which it
// applies with win.Update.
package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/proto"
)

// DefaultStopDelay is how long a plugin's process stays after its last
// pane closes. The wait makes a pane that is closed and opened again, or a
// session the user switches away from and back to, keep its plugin warm
// without a restart.
const DefaultStopDelay = 5 * time.Second

// DefaultHelloTimeout is how long a plugin may take to answer the
// handshake. A plugin that does not answer is a crash, not a wait.
const DefaultHelloTimeout = 5 * time.Second

// DefaultStallTimeout is how long one write to a plugin may take. A plugin
// that stops reading its stdin fills the pipe, and a write to a full pipe
// never returns: the wait becomes a crash, and the pane offers a restart.
const DefaultStallTimeout = 5 * time.Second

// Theme is what the app tells a plugin about how it looks: whether the
// theme is dark, and the accent color as the theme names it.
type Theme struct {
	Dark   bool
	Accent string
}

// Pane is one pane of a plugin: the state it came back with, and the size
// the app gave it.
type Pane struct {
	ID     string
	Widget string
	State  json.RawMessage
	Size   protocol.Size
}

// Callbacks are how the host tells the app what a plugin did. They run on
// the goroutine of the plugin the message came from, so the app applies
// them with win.Update.
type Callbacks struct {
	// Ready is a plugin that answered the handshake, after a start or a
	// restart. The app opens its panes again.
	Ready func(plugin string)
	// Render is a tree the plugin drew for a pane.
	Render func(plugin, pane string, tree *protocol.Tree)
	// State is state the plugin saved for a pane. The app writes it to
	// the pane's target, so the pane comes back after a restart.
	State func(plugin, pane string, state json.RawMessage)
	// Notify is a notification the plugin asked for.
	Notify func(plugin, title, body string)
	// OpenURL is a link the plugin asked to open. The URL is http or
	// https.
	OpenURL func(plugin, url string)
	// Crash is a plugin that died, with the reason and the path of its
	// log. The app shows it, and offers a restart; it does not restart
	// on its own.
	Crash func(plugin, message, logPath string)
}

// Config is what a Host needs: where its files live, which plugins the app
// knows, and how it hears back.
type Config struct {
	// Dir holds the plugin and log folders: proto.Dir().
	Dir string
	// Plugins is the plugins the app knows, as the registry lists them.
	Plugins []registry.Plugin
	// Callbacks may be nil in a test that only checks the process.
	Callbacks Callbacks
	// StopDelay, HelloTimeout and StallTimeout default when they are zero.
	StopDelay    time.Duration
	HelloTimeout time.Duration
	StallTimeout time.Duration
	// Command starts a plugin, when set. The app leaves it nil; a test
	// uses it to run a plugin without a registry of its own.
	Command func(registry.Plugin) ([]string, error)
}

// Host runs the plugins of an app. Make one with New, and Stop it when the
// app closes.
type Host struct {
	dir          string
	stopDelay    time.Duration
	helloTimeout time.Duration
	stallTimeout time.Duration
	cbs          Callbacks
	store        *Store
	command      func(registry.Plugin) ([]string, error)

	mu      sync.Mutex
	plugins map[string]registry.Plugin
	procs   map[string]*proc
	panes   map[string]map[string]Pane
	owner   map[string]string
	timers  map[string]*time.Timer
	theme   Theme
	stopped bool
}

// New makes a host for the plugins the app knows.
func New(cfg Config) *Host {
	dir := cfg.Dir
	if dir == "" {
		dir = proto.Dir()
	}
	h := &Host{
		dir:          dir,
		stopDelay:    cfg.StopDelay,
		helloTimeout: cfg.HelloTimeout,
		stallTimeout: cfg.StallTimeout,
		cbs:          cfg.Callbacks,
		store:        NewStore(filepath.Join(dir, "plugins")),
		command:      cfg.Command,
		plugins:      map[string]registry.Plugin{},
		procs:        map[string]*proc{},
		panes:        map[string]map[string]Pane{},
		owner:        map[string]string{},
		timers:       map[string]*time.Timer{},
	}
	if h.stopDelay <= 0 {
		h.stopDelay = DefaultStopDelay
	}
	if h.helloTimeout <= 0 {
		h.helloTimeout = DefaultHelloTimeout
	}
	if h.stallTimeout <= 0 {
		h.stallTimeout = DefaultStallTimeout
	}
	for _, p := range cfg.Plugins {
		h.plugins[p.Manifest.ID] = p
	}
	return h
}

// Store is where the settings and secrets of the plugins live.
func (h *Host) Store() *Store { return h.store }

// Plugins returns the plugins the app knows, by name.
func (h *Host) Plugins() []registry.Plugin {
	h.mu.Lock()
	defer h.mu.Unlock()
	plugins := make([]registry.Plugin, 0, len(h.plugins))
	for _, p := range h.plugins {
		plugins = append(plugins, p)
	}
	sortPlugins(plugins)
	return plugins
}

// LogPath is where a plugin's stderr goes.
func (h *Host) LogPath(plugin string) string { return filepath.Join(h.logDir(), plugin+".log") }

func (h *Host) logDir() string { return filepath.Join(h.dir, "logs", "plugins") }

// Open opens a pane of a plugin, and starts the plugin's process if it is
// not running. A pane that is already open is only resized.
func (h *Host) Open(plugin string, pane Pane) error {
	h.mu.Lock()
	p, ok := h.plugins[plugin]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("no plugin %q", plugin)
	}
	if h.stopped {
		h.mu.Unlock()
		return errors.New("the app is closing")
	}
	if h.panes[plugin] == nil {
		h.panes[plugin] = map[string]Pane{}
	}
	previous, existed := h.panes[plugin][pane.ID]
	h.panes[plugin][pane.ID] = pane
	h.owner[pane.ID] = plugin
	if t := h.timers[plugin]; t != nil {
		t.Stop()
		delete(h.timers, plugin)
	}
	pr := h.procs[plugin]
	fresh := false
	if pr == nil {
		pr = h.startLocked(p)
		fresh = true
	}
	h.mu.Unlock()

	if existed && !fresh {
		if previous.Widget == pane.Widget && previous.Size == pane.Size {
			return nil
		}
		pr.send(resizeCmd{pane: pane.ID, size: pane.Size})
		return nil
	}
	pr.send(openCmd{pane: pane})
	return nil
}

// Close closes a pane of a plugin. The plugin hears close, and its process
// stops a few seconds after its last pane closes.
func (h *Host) Close(pane string) {
	h.mu.Lock()
	plugin, ok := h.owner[pane]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(h.owner, pane)
	if panes := h.panes[plugin]; panes != nil {
		delete(panes, pane)
		if len(panes) == 0 {
			delete(h.panes, plugin)
			h.timers[plugin] = time.AfterFunc(h.stopDelay, func() { h.stopIfIdle(plugin) })
		}
	}
	pr := h.procs[plugin]
	h.mu.Unlock()
	if pr != nil {
		pr.send(closeCmd{pane: pane})
	}
}

// Resize tells a plugin the size of one of its panes.
func (h *Host) Resize(pane string, size protocol.Size) {
	h.mu.Lock()
	plugin, ok := h.owner[pane]
	if !ok {
		h.mu.Unlock()
		return
	}
	if panes := h.panes[plugin]; panes != nil {
		p := panes[pane]
		p.Size = size
		panes[pane] = p
	}
	pr := h.procs[plugin]
	h.mu.Unlock()
	if pr != nil {
		pr.send(resizeCmd{pane: pane, size: size})
	}
}

// Visible tells a plugin whether one of its panes is shown or hidden. A
// plugin may save work while it is hidden.
func (h *Host) Visible(pane string, visible bool) {
	h.mu.Lock()
	plugin, ok := h.owner[pane]
	pr := h.procs[plugin]
	h.mu.Unlock()
	if ok && pr != nil {
		pr.send(visibleCmd{pane: pane, visible: visible})
	}
}

// Event tells a plugin what the user did in one of its panes.
func (h *Host) Event(pane, node, kind string, value any) {
	h.mu.Lock()
	plugin, ok := h.owner[pane]
	pr := h.procs[plugin]
	h.mu.Unlock()
	if ok && pr != nil {
		pr.send(eventCmd{pane: pane, node: node, kind: kind, value: value})
	}
}

// Command tells a plugin the user ran one of its commands.
func (h *Host) Command(pane, id string) {
	h.mu.Lock()
	plugin, ok := h.owner[pane]
	pr := h.procs[plugin]
	h.mu.Unlock()
	if ok && pr != nil {
		pr.send(commandCmd{pane: pane, id: id})
	}
}

// SetTheme tells every running plugin how the app looks now.
func (h *Host) SetTheme(theme Theme) {
	h.mu.Lock()
	h.theme = theme
	procs := h.procListLocked()
	h.mu.Unlock()
	for _, pr := range procs {
		pr.send(themeCmd{theme: theme})
	}
}

// SendSettings sends a plugin the settings it may see: the values of the
// fields its manifest declares, except the secret ones. Secrets never
// travel in settings; a plugin asks for one with secret.get.
func (h *Host) SendSettings(plugin string) {
	h.mu.Lock()
	p, ok := h.plugins[plugin]
	pr := h.procs[plugin]
	h.mu.Unlock()
	if !ok {
		return
	}
	if pr != nil {
		pr.send(settingsCmd{values: h.settingsFor(p)})
	}
}

// Restart starts a crashed plugin again. The app offers this as a button;
// nothing restarts a plugin on its own.
func (h *Host) Restart(plugin string) error {
	h.mu.Lock()
	p, ok := h.plugins[plugin]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("no plugin %q", plugin)
	}
	if h.stopped {
		h.mu.Unlock()
		return errors.New("the app is closing")
	}
	old := h.procs[plugin]
	delete(h.procs, plugin)
	delete(h.panes, plugin)
	for pane, owner := range h.owner {
		if owner == plugin {
			delete(h.owner, pane)
		}
	}
	if t := h.timers[plugin]; t != nil {
		t.Stop()
		delete(h.timers, plugin)
	}
	h.mu.Unlock()
	if old != nil {
		old.stop()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return nil
	}
	h.startLocked(p)
	return nil
}

// Running tells whether a plugin's process is up.
func (h *Host) Running(plugin string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.procs[plugin] != nil
}

// Stop stops every plugin. The app calls it when it quits.
func (h *Host) Stop() {
	h.mu.Lock()
	h.stopped = true
	procs := h.procListLocked()
	h.procs = map[string]*proc{}
	h.panes = map[string]map[string]Pane{}
	h.owner = map[string]string{}
	for id, t := range h.timers {
		t.Stop()
		delete(h.timers, id)
	}
	h.mu.Unlock()
	for _, pr := range procs {
		pr.stop()
	}
}

func (h *Host) procListLocked() []*proc {
	procs := make([]*proc, 0, len(h.procs))
	for _, pr := range h.procs {
		procs = append(procs, pr)
	}
	return procs
}

// startLocked starts one process for a plugin. The caller holds the lock.
func (h *Host) startLocked(p registry.Plugin) *proc {
	pr := &proc{
		h:       h,
		plugin:  p,
		logPath: h.LogPath(p.Manifest.ID),
		cmds:    make(chan any, 128),
		done:    make(chan struct{}),
		hello:   make(chan *protocol.Response, 1),
		readErr: make(chan error, 1),
	}
	h.procs[p.Manifest.ID] = pr
	go pr.run()
	return pr
}

// stopIfIdle stops a plugin whose last pane has stayed closed long enough.
func (h *Host) stopIfIdle(plugin string) {
	h.mu.Lock()
	if h.stopped || len(h.panes[plugin]) > 0 {
		h.mu.Unlock()
		return
	}
	pr := h.procs[plugin]
	delete(h.procs, plugin)
	delete(h.timers, plugin)
	h.mu.Unlock()
	if pr != nil {
		pr.stop()
	}
}

func (h *Host) commandFor(p registry.Plugin) ([]string, error) {
	if h.command != nil {
		return h.command(p)
	}
	return registry.Argv(p)
}

func (h *Host) settingsFor(p registry.Plugin) map[string]string {
	stored := h.store.Settings(p.Manifest.ID)
	values := map[string]string{}
	for _, setting := range p.Manifest.Settings {
		if setting.Secret {
			continue
		}
		if value, ok := stored[setting.Key]; ok {
			values[setting.Key] = value
		}
	}
	return values
}

func (h *Host) currentTheme() Theme {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.theme
}

func (h *Host) ready(plugin string) {
	h.mu.Lock()
	stopped := h.stopped
	h.mu.Unlock()
	if !stopped && h.cbs.Ready != nil {
		h.cbs.Ready(plugin)
	}
}

// crashed forgets a plugin that died, and tells the app. Nothing restarts
// it: the app shows the reason and a restart button.
func (h *Host) crashed(pr *proc, message string) {
	plugin := pr.plugin.Manifest.ID
	h.mu.Lock()
	if h.procs[plugin] == pr {
		delete(h.procs, plugin)
	}
	delete(h.panes, plugin)
	for pane, owner := range h.owner {
		if owner == plugin {
			delete(h.owner, pane)
		}
	}
	if t := h.timers[plugin]; t != nil {
		t.Stop()
		delete(h.timers, plugin)
	}
	stopped := h.stopped
	h.mu.Unlock()
	if stopped {
		return
	}
	if h.cbs.Crash != nil {
		h.cbs.Crash(plugin, message, pr.logPath)
	}
}

func (h *Host) onRender(plugin, pane string, tree *protocol.Tree) {
	if h.cbs.Render != nil {
		h.cbs.Render(plugin, pane, tree)
	}
}

func (h *Host) onState(plugin, pane string, state json.RawMessage) {
	if h.cbs.State != nil {
		h.cbs.State(plugin, pane, state)
	}
}

func (h *Host) onNotify(plugin, title, body string) {
	if h.cbs.Notify != nil {
		h.cbs.Notify(plugin, title, body)
	}
}

func (h *Host) onOpenURL(plugin, url string) {
	if h.cbs.OpenURL != nil {
		h.cbs.OpenURL(plugin, url)
	}
}

// proc is one plugin's process, with the goroutines that feed it and read
// it.
type proc struct {
	h       *Host
	plugin  registry.Plugin
	logPath string

	cmds    chan any
	done    chan struct{}
	hello   chan *protocol.Response
	readErr chan error

	stopOnce  sync.Once
	crashOnce sync.Once

	// mu guards the process ends: the run goroutine sets them once the
	// process starts, and the stop of a pane that closes at that moment
	// reads them.
	mu    sync.Mutex
	log   *logWriter
	cmd   *exec.Cmd
	stdin io.WriteCloser
	enc   *protocol.Encoder
}

// setLog records the log the process writes its stderr to.
func (p *proc) setLog(w *logWriter) {
	p.mu.Lock()
	p.log = w
	p.mu.Unlock()
}

// setPipe records the ends of a process that has just started.
func (p *proc) setPipe(cmd *exec.Cmd, stdin io.WriteCloser, enc *protocol.Encoder) {
	p.mu.Lock()
	p.cmd, p.stdin, p.enc = cmd, stdin, enc
	p.mu.Unlock()
}

// encoder is the plugin's stdin encoder, or nil before the process starts.
func (p *proc) encoder() *protocol.Encoder {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enc
}

type (
	openCmd struct {
		pane Pane
	}
	closeCmd struct {
		pane string
	}
	resizeCmd struct {
		pane string
		size protocol.Size
	}
	visibleCmd struct {
		pane    string
		visible bool
	}
	themeCmd struct {
		theme Theme
	}
	settingsCmd struct {
		values map[string]string
	}
	eventCmd struct {
		pane, node, kind string
		value            any
	}
	commandCmd struct {
		pane, id string
	}
	stopCmd     struct{}
	incomingCmd struct {
		req *protocol.Request
	}
)

// send hands a command to the process goroutine, and never waits for it:
// the app's calls come from the UI goroutine. A full queue means the plugin
// stopped reading its stdin, so the process is ended and the pane shows the
// crash with its Restart button.
func (p *proc) send(c any) {
	select {
	case p.cmds <- c:
	case <-p.done:
	default:
		// Off the caller's goroutine: the crash callback belongs to the
		// app, not to the frame that found the plugin stuck.
		go p.crash(fmt.Errorf("the %s plugin stopped reading from the app", p.plugin.Manifest.Name))
	}
}

// write encodes one message to the plugin, and gives up on a plugin that
// stops reading its stdin. The write runs in a goroutine of its own: a
// pipe with a full buffer blocks a write forever, and the process must be
// ended instead.
func (p *proc) write(v any) {
	enc := p.encoder()
	if enc == nil {
		return
	}
	name := p.plugin.Manifest.Name
	done := make(chan error, 1)
	go func() { done <- enc.Encode(v) }()
	select {
	case err := <-done:
		if err != nil && !p.stopped() {
			p.crash(fmt.Errorf("could not write to the %s plugin: %w", name, err))
		}
	case <-time.After(p.h.stallTimeout):
		p.crash(fmt.Errorf("the %s plugin stopped reading from the app", name))
	case <-p.done:
	}
}

func (p *proc) stopped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// stop ends the process, once. It is safe to call from the UI goroutine
// and from the process goroutine.
func (p *proc) stop() {
	p.stopOnce.Do(func() {
		close(p.done)
		p.mu.Lock()
		cmd, stdin, logw := p.cmd, p.stdin, p.log
		p.mu.Unlock()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if stdin != nil {
			_ = stdin.Close()
		}
		if logw != nil {
			_ = logw.Close()
		}
	})
}

// crash tells the app that the plugin died, then stops it, once.
func (p *proc) crash(err error) {
	p.crashOnce.Do(func() {
		p.h.crashed(p, err.Error())
	})
	p.stop()
}

// run is the goroutine of one plugin process: it starts the process, greets
// it, and carries commands to it until it dies or is stopped.
func (p *proc) run() {
	name := p.plugin.Manifest.Name
	argv, err := p.h.commandFor(p.plugin)
	if err != nil {
		p.crash(fmt.Errorf("could not start the %s plugin: %w", name, err))
		return
	}
	dir := p.h.logDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		p.crash(fmt.Errorf("could not make the plugin log folder: %w", err))
		return
	}
	logw, err := openLog(dir, p.plugin.Manifest.ID)
	if err != nil {
		p.crash(fmt.Errorf("could not open the log of the %s plugin: %w", name, err))
		return
	}
	p.setLog(logw)
	cmd := exec.Command(argv[0], argv[1:]...)
	if p.plugin.Dir != "" && !p.plugin.Builtin {
		cmd.Dir = p.plugin.Dir
	}
	cmd.Stderr = logw
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.crash(fmt.Errorf("could not read from the %s plugin: %w", name, err))
		return
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.crash(fmt.Errorf("could not write to the %s plugin: %w", name, err))
		return
	}
	if err := cmd.Start(); err != nil {
		p.crash(fmt.Errorf("could not start the %s plugin: %w", name, err))
		return
	}
	p.setPipe(cmd, stdin, protocol.NewEncoder(stdin))
	// The pane may have closed while the process was starting; the stop
	// that closed it saw no process to kill, so this one ends it here.
	if p.stopped() {
		_ = cmd.Process.Kill()
		_ = stdin.Close()
		return
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	go p.read(protocol.NewDecoder(stdout))

	greeting, err := protocol.NewRequest(1, protocol.MethodHello, protocol.HelloParams{Protocol: protocol.Version, Host: "shibu"})
	if err != nil {
		p.crash(err)
		return
	}
	p.write(greeting)
	if p.stopped() {
		return
	}

	timer := time.NewTimer(p.h.helloTimeout)
	defer timer.Stop()
	ready := false
	var queue []any
	for {
		select {
		case c := <-p.cmds:
			if !ready {
				queue = append(queue, c)
				continue
			}
			p.handle(c)
			if p.stopped() {
				return
			}
		case resp := <-p.hello:
			if ready || resp == nil {
				continue
			}
			ready = true
			if resp.Error != nil {
				p.crash(fmt.Errorf("the %s plugin refused the handshake: %s", name, resp.Error.Message))
				return
			}
			var hello protocol.HelloResult
			if err := json.Unmarshal(resp.Result, &hello); err != nil {
				p.crash(fmt.Errorf("the %s plugin sent a bad handshake: %w", name, err))
				return
			}
			if err := protocol.CheckVersion(hello.Protocol); err != nil {
				p.crash(fmt.Errorf("the %s plugin speaks protocol %d; this app speaks %d", name, hello.Protocol, protocol.Version))
				return
			}
			p.h.ready(p.plugin.Manifest.ID)
			theme := p.h.currentTheme()
			p.tell(protocol.MethodTheme, protocol.ThemeParams{Dark: theme.Dark, Accent: theme.Accent})
			p.tell(protocol.MethodSettings, protocol.SettingsParams{Values: p.h.settingsFor(p.plugin)})
			for _, c := range queue {
				p.handle(c)
			}
			queue = nil
			if p.stopped() {
				return
			}
		case err := <-wait:
			if p.stopped() {
				return
			}
			if err != nil {
				p.crash(fmt.Errorf("the %s plugin exited: %w", name, err))
			} else {
				p.crash(fmt.Errorf("the %s plugin exited", name))
			}
			return
		case err := <-p.readErr:
			if p.stopped() {
				return
			}
			if errors.Is(err, io.EOF) {
				p.crash(fmt.Errorf("the %s plugin exited", name))
				return
			}
			p.crash(fmt.Errorf("could not read from the %s plugin: %w", name, err))
			return
		case <-timer.C:
			if !ready {
				p.crash(fmt.Errorf("the %s plugin did not answer the handshake within %s", name, p.h.helloTimeout))
				return
			}
		case <-p.done:
			return
		}
	}
}

// read decodes the plugin's lines: the replies to the host, and the calls
// the plugin makes. A line that is not JSON gets a parse error back and the
// plugin keeps its process; a line too long is refused the same way.
func (p *proc) read(dec *protocol.Decoder) {
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, protocol.ErrLineTooLong) {
			p.parseFail(protocol.CodeParse, "the line was too long")
			continue
		}
		if err != nil {
			// A line that is not JSON is a bad message, which the
			// protocol answers and forgets. Only a read error ends the
			// process.
			var syntax *json.SyntaxError
			if errors.As(err, &syntax) {
				p.parseFail(protocol.CodeParse, "parse error")
				continue
			}
			p.readErr <- err
			return
		}
		var probe struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			p.parseFail(protocol.CodeParse, "parse error")
			continue
		}
		if probe.Method == "" {
			var resp protocol.Response
			if err := json.Unmarshal(raw, &resp); err != nil {
				p.parseFail(protocol.CodeParse, "parse error")
				continue
			}
			select {
			case p.hello <- &resp:
			default:
			}
			continue
		}
		var req protocol.Request
		if err := json.Unmarshal(raw, &req); err != nil {
			p.parseFail(protocol.CodeParse, "parse error")
			continue
		}
		p.send(incomingCmd{req: &req})
	}
}

func (p *proc) tell(method string, params any) {
	req, err := protocol.NewNotification(method, params)
	if err != nil {
		return
	}
	p.write(req)
}

func (p *proc) ok(req *protocol.Request) {
	if req.IsNotification() {
		return
	}
	resp, err := protocol.NewResult(req.ID, struct{}{})
	if err != nil {
		return
	}
	p.write(resp)
}

// fail answers a request with an error. A notification has nobody to
// answer, so its call is refused without a reply.
func (p *proc) fail(id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		return
	}
	p.write(protocol.NewFailure(id, code, message))
}

// parseFail answers a line that did not read as a message. It has no id to
// answer to, so the id is null, as JSON-RPC asks.
func (p *proc) parseFail(code int, message string) {
	p.write(protocol.NewFailure(nil, code, message))
}

// handle applies one host command to the process.
func (p *proc) handle(c any) {
	switch c := c.(type) {
	case openCmd:
		p.tell(protocol.MethodOpen, protocol.OpenParams{Pane: c.pane.ID, Widget: c.pane.Widget, State: c.pane.State, Size: c.pane.Size})
	case closeCmd:
		p.tell(protocol.MethodClose, protocol.CloseParams{Pane: c.pane})
	case resizeCmd:
		p.tell(protocol.MethodResize, protocol.ResizeParams{Pane: c.pane, Size: c.size})
	case visibleCmd:
		p.tell(protocol.MethodVisible, protocol.VisibleParams{Pane: c.pane, Visible: c.visible})
	case themeCmd:
		p.tell(protocol.MethodTheme, protocol.ThemeParams{Dark: c.theme.Dark, Accent: c.theme.Accent})
	case settingsCmd:
		p.tell(protocol.MethodSettings, protocol.SettingsParams{Values: c.values})
	case eventCmd:
		p.tell(protocol.MethodEvent, protocol.EventParams{Pane: c.pane, Node: c.node, Kind: c.kind, Value: c.value})
	case commandCmd:
		p.tell(protocol.MethodCommand, protocol.CommandParams{ID: c.id})
	case incomingCmd:
		p.answer(c.req)
	case stopCmd:
		p.stop()
	}
}

// answer replies to one call of the plugin. Every call is checked against
// the permissions the plugin's manifest asked for; a call without its
// permission is refused, not run.
func (p *proc) answer(req *protocol.Request) {
	name := p.plugin.Manifest.Name
	if req.Method == protocol.MethodHello {
		p.fail(req.ID, protocol.CodeInvalidRequest, "hello comes from the app, not the plugin")
		return
	}
	// An unknown method is no call the app knows, before permissions:
	// there is no permission that would make it run.
	if _, known := protocol.MethodPermission[req.Method]; !known {
		p.fail(req.ID, protocol.CodeNoMethod, fmt.Sprintf("unknown method %q", req.Method))
		return
	}
	if !p.plugin.Manifest.Permissions.Allows(req.Method) {
		p.fail(req.ID, protocol.CodeInvalidRequest, fmt.Sprintf("the %s plugin did not ask for the permission %q needs", name, req.Method))
		return
	}
	switch req.Method {
	case protocol.MethodRender:
		var params protocol.RenderParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "render: "+err.Error())
			return
		}
		if params.Pane == "" {
			p.fail(req.ID, protocol.CodeInvalidParams, "render: no pane")
			return
		}
		if params.Tree.Root == nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "render: no tree")
			return
		}
		if err := params.Tree.ValidateTree(); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "render: "+err.Error())
			return
		}
		if req.IsNotification() {
			p.h.onRender(p.plugin.Manifest.ID, params.Pane, &params.Tree)
			return
		}
		p.h.onRender(p.plugin.Manifest.ID, params.Pane, &params.Tree)
		p.ok(req)
	case protocol.MethodStateSave:
		var params protocol.StateSaveParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "state.save: "+err.Error())
			return
		}
		if params.Pane == "" {
			p.fail(req.ID, protocol.CodeInvalidParams, "state.save: no pane")
			return
		}
		if len(params.State) > 0 && !json.Valid(params.State) {
			p.fail(req.ID, protocol.CodeInvalidParams, "state.save: state is not JSON")
			return
		}
		if req.IsNotification() {
			p.h.onState(p.plugin.Manifest.ID, params.Pane, params.State)
			return
		}
		p.h.onState(p.plugin.Manifest.ID, params.Pane, params.State)
		p.ok(req)
	case protocol.MethodSecretGet:
		var params protocol.SecretGetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "secret.get: "+err.Error())
			return
		}
		value := p.h.store.Secret(p.plugin.Manifest.ID, params.Key)
		if req.IsNotification() {
			return
		}
		resp, err := protocol.NewResult(req.ID, protocol.SecretGetResult{Value: value})
		if err != nil {
			p.fail(req.ID, protocol.CodeInternal, err.Error())
			return
		}
		p.write(resp)
	case protocol.MethodSecretSet:
		var params protocol.SecretSetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "secret.set: "+err.Error())
			return
		}
		if err := p.h.store.SetSecret(p.plugin.Manifest.ID, params.Key, params.Value); err != nil {
			p.fail(req.ID, protocol.CodeInternal, err.Error())
			return
		}
		p.ok(req)
	case protocol.MethodNotify:
		var params protocol.NotifyParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "notify: "+err.Error())
			return
		}
		p.h.onNotify(p.plugin.Manifest.ID, params.Title, params.Body)
		p.ok(req)
	case protocol.MethodOpenURL:
		var params protocol.OpenURLParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			p.fail(req.ID, protocol.CodeInvalidParams, "openURL: "+err.Error())
			return
		}
		if !webURL(params.URL) {
			p.fail(req.ID, protocol.CodeInvalidParams, "openURL: only http and https links open")
			return
		}
		p.h.onOpenURL(p.plugin.Manifest.ID, params.URL)
		p.ok(req)
	default:
		p.fail(req.ID, protocol.CodeNoMethod, fmt.Sprintf("unknown method %q", req.Method))
	}
}

func sortPlugins(plugins []registry.Plugin) {
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Manifest.Name < plugins[j].Manifest.Name })
}

// webURL tells a link the app may open: http or https, and a host.
func webURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
