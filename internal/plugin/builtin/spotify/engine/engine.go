// Package engine plays Spotify in the app itself, with no Spotify desktop
// app running. It runs Spotify's Web Playback SDK in a page, and the page
// runs in a web view with no window that the host owns (the plugin's web
// permission); the plugin never touches the web view, only a small server
// of its own on this machine's loopback address.
//
// The server serves the page, gives it the access token when it asks, takes
// the player's state as the page posts it, and sends the page the commands
// of the pane over server-sent events. The address holds a random key, and
// a request that is not for the server's own address is refused, so no web
// page the user visits can reach it. The token goes from the plugin to its
// own page and nowhere else.
//
// An Engine is a [transport.Player]: the pane controls it as it controls the
// desktop app. It pushes a snapshot when the player's state changes, so it
// needs no polling.
package engine

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
)

//go:embed page.html
var pageHTML []byte

// webID is the id of the engine's web view in the host.
const webID = "spotify-engine"

var (
	// ErrStarting says the player is not ready yet: its page is loading
	// or connecting.
	ErrStarting = errors.New("the playback engine is starting")

	// ErrClosed says the engine was closed.
	ErrClosed = errors.New("the playback engine is closed")
)

// PlaybackError is an error that the page reported: Spotify's player or one
// of its calls failed. Kind is what failed (account_error, playback_error,
// command and so on) and Message is what Spotify or the page said.
type PlaybackError struct {
	Kind    string
	Message string
}

func (e *PlaybackError) Error() string {
	return fmt.Sprintf("spotify engine: %s: %s", e.Kind, e.Message)
}

// PremiumRequired tells whether Spotify refused because the account is not
// Premium, which the player reports as an account error or as a 403 with
// the reason PREMIUM_REQUIRED.
func (e *PlaybackError) PremiumRequired() bool {
	return e.Kind == "account_error" || strings.Contains(e.Message, "PREMIUM_REQUIRED")
}

// SignInRequired tells whether Spotify refused the token, or the plugin could
// not give the page one, so that the user has to sign in again: an
// authentication error, a failed token or a 401 answer.
func (e *PlaybackError) SignInRequired() bool {
	return e.Kind == "authentication_error" || e.Kind == "token" || strings.Contains(e.Message, "Spotify answered 401")
}

// uriRe is a Spotify URI the engine plays: a track, an album, a playlist, an
// artist, a show or an episode.
var uriRe = regexp.MustCompile(`^spotify:(track|album|playlist|artist|show|episode):[A-Za-z0-9]{1,64}$`)

// trackRe is a Spotify URI of a track, the offset of a play in a context.
var trackRe = regexp.MustCompile(`^spotify:track:[A-Za-z0-9]{1,64}$`)

// Web runs the engine's page in a web view with no window. The plugin's
// connection to its host is one.
type Web interface {
	WebOpen(id, url string) error
	WebClose(id string) error
}

// Config is what an Engine needs.
type Config struct {
	Web Web
	// Token returns a valid access token for the page, which asks again
	// whenever the SDK or a call does.
	Token func(ctx context.Context) (string, error)
	// Emit takes the player's snapshot each time its state changes.
	Emit func(transport.Snapshot)
	// OnError takes an error the player reported, or ErrStarting when the
	// player went away.
	OnError func(error)
	// Name is how the player shows in Spotify's list of devices. Empty is
	// "Shibu Code".
	Name string
	// Now is the clock, for tests. Nil is time.Now.
	Now func() time.Time
}

// Engine is one player.
type Engine struct {
	cfg Config
	key string

	mu     sync.Mutex
	srv    *http.Server
	ready  bool
	closed bool
	// streams is how many command streams are open. The page has one;
	// when the last one ends, the page cannot take commands.
	streams int
	// page and seq order the events that say whether the page can take
	// commands: the id of the page that sent the last one, and its number.
	page  string
	seq   int
	snap  transport.Snapshot
	queue []queued
	wake  chan struct{}
	done  chan struct{}

	// lastPlay is the URI (and the offset) of the last play command and
	// when it was sent, to drop a double click.
	lastPlay   string
	lastPlayAt time.Time
}

// queued is a command that waits for the page to take it.
type queued struct {
	cmd string
	raw []byte
	at  time.Time
}

// staleAfter is how long a play command waits for the page before it is
// too old to run: a track that starts a minute after the click is a
// surprise, not an answer.
const staleAfter = 5 * time.Second

// maxQueue is how many commands wait for the page at most.
const maxQueue = 32

// playWindow is how long an identical play command after another is a
// double click, and dropped.
const playWindow = time.Second

// New returns an engine that has not started.
func New(cfg Config) *Engine {
	if cfg.Name == "" {
		cfg.Name = "Shibu Code"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return &Engine{
		cfg:  cfg,
		key:  hex.EncodeToString(raw),
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

// Start serves the page and asks the host to run it. It does nothing when
// the engine runs already.
func (e *Engine) Start() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	if e.srv != nil {
		e.mu.Unlock()
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		e.mu.Unlock()
		return err
	}
	addr := ln.Addr().String()
	e.srv = &http.Server{Handler: e.handler(addr), ReadHeaderTimeout: 5 * time.Second}
	srv := e.srv
	e.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()

	page := "http://" + addr + "/e/" + e.key + "/?name=" + url.QueryEscape(e.cfg.Name)
	log.Printf("engine: page served on %s", addr)
	if err := e.cfg.Web.WebOpen(webID, page); err != nil {
		_ = e.Close()
		return err
	}
	return nil
}

// Close stops the server and ends the page. The player stops with it.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.ready = false
	srv := e.srv
	close(e.done)
	e.mu.Unlock()
	var err error
	if srv != nil {
		err = srv.Close()
		if werr := e.cfg.Web.WebClose(webID); werr != nil && err == nil {
			err = werr
		}
	}
	return err
}

// handler serves the page, the token, the events and the commands, each
// only at this engine's own address and key.
func (e *Engine) handler(addr string) http.Handler {
	prefix := "/e/" + e.key + "/"
	mux := http.NewServeMux()
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(pageHTML)
	})
	mux.HandleFunc(prefix+"token", func(w http.ResponseWriter, r *http.Request) {
		token, err := e.cfg.Token(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})
	mux.HandleFunc(prefix+"events", e.events)
	mux.HandleFunc(prefix+"commands", e.stream)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A page of the web that asks for this server by name (DNS
		// rebinding) or from another origin is not the engine's page.
		if r.Host != addr {
			http.Error(w, "wrong host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+addr {
			http.Error(w, "wrong origin", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

// event is what the page posts.
type event struct {
	Type     string  `json:"type"`
	Active   bool    `json:"active"`
	Paused   bool    `json:"paused"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
	Track    string  `json:"track"`
	URI      string  `json:"uri"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Image    string  `json:"image"`
	Shuffle  bool    `json:"shuffle"`
	Repeat   int     `json:"repeat"`
	Value    int     `json:"value"`
	Kind     string  `json:"kind"`
	Message  string  `json:"message"`

	// The page sends these with "ready", to tell why audio may stay silent.
	Count      int    `json:"count"`
	Visibility string `json:"visibility"`
	Audio      string `json:"audio"`

	// The page numbers its ready, not-ready and stream-up events, since
	// the posts can arrive in another order than it sent them. Page is
	// the id of one page load, and a new load starts again from 1.
	Page string `json:"page"`
	Seq  int    `json:"seq"`
}

func (e *Engine) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "post only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var ev event
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e.handle(ev)
	w.WriteHeader(http.StatusNoContent)
}

// handle applies one event of the page, and tells the plugin.
func (e *Engine) handle(ev event) {
	var emit *transport.Snapshot
	var fail error
	// The plugin's stderr is its log: what the page reports is the only
	// view of a player that has no window.
	switch ev.Type {
	case "state":
		log.Printf("engine: state active=%t paused=%t position=%.0fms track=%q", ev.Active, ev.Paused, ev.Position, ev.Track)
	case "error":
		log.Printf("engine: error %s: %s", ev.Kind, ev.Message)
	case "log":
		log.Printf("engine: page: %s", ev.Message)
	case "ready":
		log.Printf("engine: ready n=%d visibility=%q audio=%q", ev.Count, ev.Visibility, ev.Audio)
	default:
		log.Printf("engine: %s", ev.Type)
	}
	e.mu.Lock()
	if !e.orderedLocked(ev) {
		e.mu.Unlock()
		log.Printf("engine: %s event %d is older than the last, ignored", ev.Type, ev.Seq)
		return
	}
	switch ev.Type {
	case "ready":
		e.ready = true
		e.snap.ReadAt = e.cfg.Now()
		snap := e.snap
		emit = &snap
	case "stream-up":
		// The page's command stream came back while its player was still
		// connected, so no new ready follows: the page says so itself.
		e.ready = true
		snap := e.snap
		emit = &snap
	case "not-ready":
		e.ready = false
		fail = ErrStarting
	case "volume":
		e.snap.Volume = min(max(ev.Value, 0), 100)
		snap := e.snap
		emit = &snap
	case "state":
		volume := e.snap.Volume
		e.snap = transport.Snapshot{Volume: volume, ReadAt: e.cfg.Now()}
		if ev.Active {
			e.snap.State = transport.StatePlaying
			if ev.Paused {
				e.snap.State = transport.StatePaused
			}
			e.snap.Track = ev.Track
			e.snap.URI = ev.URI
			e.snap.Artist = ev.Artist
			e.snap.Album = ev.Album
			e.snap.ArtworkURL = ev.Image
			e.snap.Duration = time.Duration(ev.Duration) * time.Millisecond
			e.snap.Position = time.Duration(ev.Position) * time.Millisecond
			e.snap.Shuffle = ev.Shuffle
			e.snap.Repeat = ev.Repeat != 0
		}
		snap := e.snap
		emit = &snap
	case "error":
		fail = &PlaybackError{Kind: ev.Kind, Message: ev.Message}
	}
	e.mu.Unlock()
	if emit != nil && e.cfg.Emit != nil {
		e.cfg.Emit(*emit)
	}
	if fail != nil && e.cfg.OnError != nil {
		e.cfg.OnError(fail)
	}
}

// orderedLocked tells whether an event is not older than the last one that
// the engine applied, and notes it. Only the events that set whether the page
// is ready are numbered; a page that does not number them (Seq 0) is taken
// as it comes. It is called with e.mu held.
func (e *Engine) orderedLocked(ev event) bool {
	switch ev.Type {
	case "ready", "not-ready", "stream-up":
	default:
		return true
	}
	if ev.Seq == 0 {
		return true
	}
	if ev.Page != e.page {
		// A new page load numbers again.
		e.page, e.seq = ev.Page, 0
	}
	if ev.Seq < e.seq {
		return false
	}
	e.seq = ev.Seq
	return true
}

// stream carries the commands to the page as server-sent events. A page
// that reconnects takes up where the last one left off.
func (e *Engine) stream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	// The stream counts from before it writes, so that a stream that ends
	// at once is still counted out, and one that replaces another is
	// counted in before the other ends.
	e.mu.Lock()
	e.streams++
	e.mu.Unlock()
	defer e.streamEnded()
	_, _ = io.WriteString(w, ": ready\n\n")
	if err := rc.Flush(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			http.Error(w, "no streaming", http.StatusInternalServerError)
		}
		return
	}
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-e.wake:
			for {
				q, ok := e.pop()
				if !ok {
					break
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", q.raw); err != nil {
					// The command is not lost for the next page, unless
					// it is too old by then.
					e.requeue(q)
					return
				}
				if err := rc.Flush(); err != nil {
					// Written is not delivered: the next page gets it.
					e.requeue(q)
					return
				}
			}
		case <-keepalive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-e.done:
			return
		}
	}
}

// streamEnded notes that a command stream ended. The page that lost its last
// stream cannot take commands, so the engine is not ready until it says
// that the stream is back (or its player says ready again). A stream that
// ends because the engine closed is not news.
func (e *Engine) streamEnded() {
	e.mu.Lock()
	e.streams--
	lost := e.streams == 0 && e.ready && !e.closed
	if lost {
		e.ready = false
	}
	e.mu.Unlock()
	if !lost {
		return
	}
	log.Printf("engine: the command stream ended, the player is not ready")
	if e.cfg.OnError != nil {
		e.cfg.OnError(ErrStarting)
	}
}

// pop takes the oldest waiting command. A play that waited longer than
// staleAfter is dropped on the way: the click it answers is long past.
func (e *Engine) pop() (queued, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for len(e.queue) > 0 {
		q := e.queue[0]
		e.queue = e.queue[1:]
		if q.cmd == "play" && e.cfg.Now().Sub(q.at) > staleAfter {
			log.Printf("engine: command play dropped, it waited too long")
			continue
		}
		return q, true
	}
	return queued{}, false
}

// requeue puts a command that no page took back at the front.
func (e *Engine) requeue(q queued) {
	e.mu.Lock()
	e.queue = append([]queued{q}, e.queue...)
	e.mu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// send queues one command for the page. A seek or a volume that waits
// last in line is replaced by the newer one, because only the last counts, and a
// play of the same URI as the one just sent is a double click and dropped.
// The page runs what it takes one at a time, and does the same.
func (e *Engine) send(cmd map[string]any) error {
	e.mu.Lock()
	closed, ready := e.closed, e.ready
	e.mu.Unlock()
	switch {
	case closed:
		return ErrClosed
	case !ready:
		log.Printf("engine: command %v refused, the player is not ready", cmd["cmd"])
		return ErrStarting
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	name, _ := cmd["cmd"].(string)
	e.mu.Lock()
	if name == "play" {
		uri, _ := cmd["uri"].(string)
		if offset, _ := cmd["offset"].(string); offset != "" {
			uri += "#" + offset
		}
		now := e.cfg.Now()
		if uri == e.lastPlay && now.Sub(e.lastPlayAt) < playWindow {
			e.mu.Unlock()
			log.Printf("engine: command play dropped, the same as the last")
			return nil
		}
		e.lastPlay, e.lastPlayAt = uri, now
	}
	// Only the last command in line gives way: a seek that waits before
	// a skip is not replaced by a seek after it, which would run the two
	// in the wrong order.
	replaced := false
	if name == "seek" || name == "volume" {
		if n := len(e.queue); n > 0 && e.queue[n-1].cmd == name {
			e.queue[n-1].raw, e.queue[n-1].at = raw, e.cfg.Now()
			replaced = true
		}
	}
	if !replaced {
		if len(e.queue) >= maxQueue {
			e.mu.Unlock()
			return errors.New("the playback engine is not taking commands")
		}
		e.queue = append(e.queue, queued{name, raw, e.cfg.Now()})
	}
	e.mu.Unlock()
	if replaced {
		log.Printf("engine: command %v replaced the waiting one", name)
	} else {
		log.Printf("engine: command %v", name)
	}
	select {
	case e.wake <- struct{}{}:
	default:
	}
	return nil
}

// PlayPause flips between playing and paused. When this player does not
// play yet, it becomes the one that plays, and resumes.
func (e *Engine) PlayPause(context.Context) error { return e.send(map[string]any{"cmd": "toggle"}) }

// Next skips to the next track.
func (e *Engine) Next(context.Context) error { return e.send(map[string]any{"cmd": "next"}) }

// Previous goes to the previous track.
func (e *Engine) Previous(context.Context) error { return e.send(map[string]any{"cmd": "previous"}) }

// Launch has no app to open: the engine is the player.
func (e *Engine) Launch(context.Context) error { return transport.ErrNotInstalled }

// Seek moves to a position in the track.
func (e *Engine) Seek(_ context.Context, position time.Duration) error {
	if position < 0 {
		return transport.ErrRange
	}
	return e.send(map[string]any{"cmd": "seek", "ms": position.Milliseconds()})
}

// Volume sets the volume, from 0 to 100.
func (e *Engine) Volume(_ context.Context, volume int) error {
	if volume < 0 || volume > 100 {
		return transport.ErrRange
	}
	return e.send(map[string]any{"cmd": "volume", "value": volume})
}

// Shuffle turns shuffle on or off.
func (e *Engine) Shuffle(_ context.Context, on bool) error {
	return e.send(map[string]any{"cmd": "shuffle", "on": on})
}

// Repeat turns repeat of the context on or off.
func (e *Engine) Repeat(_ context.Context, on bool) error {
	return e.send(map[string]any{"cmd": "repeat", "on": on})
}

// Play starts a track, an album, a playlist, an artist, a show or an
// episode, given as its Spotify URI.
func (e *Engine) Play(_ context.Context, uri string) error {
	if !uriRe.MatchString(uri) {
		return fmt.Errorf("%w: %q", transport.ErrURI, uri)
	}
	return e.send(map[string]any{"cmd": "play", "uri": uri})
}

// PlayFrom starts a track inside its album or playlist, so that what
// follows it is the rest of that context. contextURI is the album or the
// playlist, and trackURI the track in it.
func (e *Engine) PlayFrom(_ context.Context, contextURI, trackURI string) error {
	if !uriRe.MatchString(contextURI) {
		return fmt.Errorf("%w: %q", transport.ErrURI, contextURI)
	}
	if !trackRe.MatchString(trackURI) {
		return fmt.Errorf("%w: %q", transport.ErrURI, trackURI)
	}
	return e.send(map[string]any{"cmd": "play", "uri": contextURI, "offset": trackURI})
}

// Read returns the last state the player pushed.
func (e *Engine) Read(context.Context) (transport.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return transport.Snapshot{}, ErrClosed
	}
	if !e.ready {
		return transport.Snapshot{}, ErrStarting
	}
	return e.snap, nil
}

var _ transport.Player = (*Engine)(nil)
