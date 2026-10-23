package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/locales"
)

// contextEngine is the fake engine that starts a track inside a context, and
// records what it was asked.
type contextEngine struct {
	desktopEngine
	mu    sync.Mutex
	plays [][2]string
}

func (e *contextEngine) PlayFrom(_ context.Context, contextURI, trackURI string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.plays = append(e.plays, [2]string{contextURI, trackURI})
	return nil
}

func (e *contextEngine) played() [][2]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][2]string(nil), e.plays...)
}

// trackJSON is a track as the Web API writes it.
func trackJSON(id string) string {
	return fmt.Sprintf(`{"id":%q,"uri":"spotify:track:%s","name":"Song %s","type":"track","duration_ms":61000,"artists":[{"name":"Ada"}],"album":{"name":"Alb","images":[]}}`, id, id, id)
}

// tracksHandler answers the tracks of the album a1, the playlist p1 and the
// queue, and refuses the playlist p403.
func tracksHandler(queueCalls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/albums/a1/tracks":
			_, _ = w.Write([]byte(`{"items":[` + trackJSON("t1") + `,` + trackJSON("t2") + `]}`))
		case "/v1/playlists/p1/items":
			_, _ = w.Write([]byte(`{"items":[{"item":` + trackJSON("t1") + `},{"item":` + trackJSON("t1") + `}]}`))
		case "/v1/playlists/p403/items":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"Forbidden"}}`))
		case "/v1/me/player/queue":
			if queueCalls != nil {
				queueCalls.Add(1)
			}
			_, _ = w.Write([]byte(`{"currently_playing":` + trackJSON("t0") + `,"queue":[` + trackJSON("t3") + `,` + trackJSON("t4") + `]}`))
		default:
			http.NotFound(w, r)
		}
	})
}

// drillPlugin is a plugin at the large size with a library and a fake engine
// that plays inside a context.
func drillPlugin(t *testing.T) (*plugin, *session, *contextEngine, *recordingRunner) {
	t.Helper()
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	eng := &contextEngine{desktopEngine: desktopEngine{p.desktop}}
	p.engine, p.player = eng, eng
	p.library = libraryClient(t, tracksHandler(nil))
	s.tab = tabAlbums
	s.entries = []entry{
		{id: "album-a1", title: "Album One", subtitle: "Ada", image: "https://i/300", uri: "spotify:album:a1"},
		{id: "playlist-p1", title: "Mix", uri: "spotify:playlist:p1"},
		{id: "playlist-p403", title: "Theirs", uri: "spotify:playlist:p403"},
	}
	return p, s, eng, runner
}

func drillReady(t *testing.T, p *plugin, s *session) {
	t.Helper()
	eventually(t, "the tracks", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.tracksState != tracksLoading
	})
}

// A tile opens its tracks, with a back button, the name, Play, and a row for
// each track: its number, title, artists and length. The row of the track
// that plays is marked.
func TestATileOpensItsTracks(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.mu.Lock()
	s.snap.URI = "spotify:track:t2"
	p.mu.Unlock()

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"})
	drillReady(t, p, s)

	p.mu.Lock()
	root := draw(t, p, s)
	drill, name := s.drill, s.drillName
	p.mu.Unlock()
	if drill != "album-a1" || name != "Album One" {
		t.Fatalf("drill = %q %q", drill, name)
	}
	for _, id := range []string{"drill-back", "drill-play", "drill-name"} {
		if find(root, id) == nil {
			t.Errorf("the drill-in has no %q", id)
		}
	}
	if find(root, "tabs") != nil || find(root, "results") != nil {
		t.Error("the drill-in still draws the tabs and the tiles")
	}
	if play := find(root, "drill-play"); play == nil || !play.Primary || play.Label != locales.SpotifyPlay() {
		t.Fatalf("drill-play = %+v", play)
	}
	list := find(root, "tracks")
	if list == nil || len(list.Items) != 2 {
		t.Fatalf("tracks = %+v", list)
	}
	first, second := list.Items[0], list.Items[1]
	if first.ID != "t-0" || first.Leading != "1" || first.Title != "Song t1" || first.Subtitle != "Ada" || first.Trailing != "1:01" || first.Active {
		t.Errorf("first row = %+v", first)
	}
	if !second.Active || second.Leading != "2" {
		t.Errorf("second row = %+v, want the track that plays marked", second)
	}
}

// Play plays the whole album; a row starts its track inside the album.
func TestPlayAllAndPlayInContext(t *testing.T) {
	p, s, eng, runner := drillPlugin(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"})
	drillReady(t, p, s)

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "drill-play", Kind: protocol.KindPress})
	eventuallyScript(t, runner, `play track "spotify:album:a1"`)

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tracks", Kind: protocol.KindSelect, Value: "t-1"})
	eventually(t, "the play in context", func() bool { return len(eng.played()) == 1 })
	if got := eng.played()[0]; got != [2]string{"spotify:album:a1", "spotify:track:t2"} {
		t.Fatalf("PlayFrom = %v", got)
	}
	// The pane shows the row at once.
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Song t2" || s.snap.URI != "spotify:track:t2" {
		t.Fatalf("snap = %+v, want the pressed track", s.snap)
	}
}

// A playlist that Spotify does not share says so, keeps Play, and asks to
// sign in again only when the token lacks the collaborative scope.
func TestAForbiddenPlaylistSaysSo(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "playlist-p403"})
	drillReady(t, p, s)

	p.mu.Lock()
	root := draw(t, p, s)
	state := s.tracksState
	p.mu.Unlock()
	if state != tracksForbidden {
		t.Fatalf("state = %v", state)
	}
	if note := find(root, "tracks-forbidden"); note == nil || note.Text != "Spotify doesn't share this playlist's tracks" {
		t.Fatalf("note = %+v", note)
	}
	if find(root, "drill-play") == nil || find(root, "tracks") != nil {
		t.Error("a refused playlist keeps Play and has no rows")
	}
	if find(root, "sign-in-again") == nil {
		t.Error("a token without the collaborative scope is not asked to sign in again")
	}

	// A token that has the scope is not asked: another sign-in would not help.
	p.library = scopedClient(t, tracksHandler(nil), web.ScopeCollaborative)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "drill-back", Kind: protocol.KindPress})
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "playlist-p403"})
	drillReady(t, p, s)
	p.mu.Lock()
	defer p.mu.Unlock()
	if root := draw(t, p, s); find(root, "sign-in-again") != nil || find(root, "tracks-forbidden") == nil {
		t.Fatalf("a scoped token: %q", texts(root))
	}
}

// scopedClient is a signed-in client whose token has a scope.
func scopedClient(t *testing.T, handler http.Handler, scope string) *web.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	raw, err := json.Marshal(web.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour), Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	client := web.New(web.Config{
		BaseURL:     server.URL,
		AccountsURL: server.URL,
		Secrets:     web.MapSecrets{web.TokensKey: string(raw)},
		OpenURL:     web.OpenURLFunc(func(string) error { return nil }),
	})
	client.SetClientID("client")
	return client
}

// A playlist may hold a track twice; its rows still have their own ids.
func TestPlaylistRowsKeepUniqueIDs(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "playlist-p1"})
	drillReady(t, p, s)
	p.mu.Lock()
	defer p.mu.Unlock()
	root := draw(t, p, s)
	if list := find(root, "tracks"); list == nil || len(list.Items) != 2 || list.Items[0].ID == list.Items[1].ID {
		t.Fatalf("tracks = %+v", list)
	}
}

// The drill-in is part of the saved state, and comes back after a restart.
func TestTheDrillComesBackAfterARestart(t *testing.T) {
	data, err := json.Marshal(persisted{Tab: int(tabQueue), Drill: "album-a1", DrillName: "Album One"})
	if err != nil {
		t.Fatal(err)
	}
	var saved persisted
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Tab != int(tabQueue) || saved.Drill != "album-a1" || saved.DrillName != "Album One" {
		t.Fatalf("state = %+v", saved)
	}

	p, s, _, _ := drillPlugin(t)
	fresh := &session{pane: "p1", widget: widgetPlayer, size: s.size, playback: modePlaying, lib: libReady, loaded: map[tabKind]bool{}}
	fresh.restoreDrill(saved.Drill, saved.DrillName)
	if fresh.drill != "album-a1" || fresh.drillURI != "spotify:album:a1" || fresh.drillName != "Album One" {
		t.Fatalf("restored = %+v", fresh)
	}
	p.panes["p1"] = fresh
	p.syncLibrary("p1")
	drillReady(t, p, fresh)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(fresh.tracks) != 2 {
		t.Fatalf("tracks = %+v", fresh.tracks)
	}

	// A state that names no playlist or album restores nothing.
	other := &session{}
	other.restoreDrill("artist-x", "X")
	other.restoreDrill("album-", "X")
	if other.drill != "" {
		t.Fatalf("drill = %q", other.drill)
	}
}

// Back leaves the drill-in and saves that.
func TestBackLeavesTheDrill(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"})
	drillReady(t, p, s)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "drill-back", Kind: protocol.KindPress})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.drill != "" || len(s.tracks) != 0 {
		t.Fatalf("drill = %q, tracks = %d", s.drill, len(s.tracks))
	}
	if root := draw(t, p, s); find(root, "tabs") == nil {
		t.Fatal("the tabs did not come back")
	}
}

// A failed track or queue read offers a retry in place, without making
// the user navigate away and lose the open list.
func TestFailedListsRetryInPlace(t *testing.T) {
	for _, queue := range []bool{false, true} {
		t.Run(fmt.Sprintf("queue=%v", queue), func(t *testing.T) {
			p, s, _, _ := drillPlugin(t)
			var fail atomic.Bool
			fail.Store(true)
			handler := tracksHandler(nil)
			p.library = libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if fail.Load() {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			if queue {
				p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-queue", Kind: protocol.KindPress})
			} else {
				p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"})
			}
			eventually(t, "the failed list", func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return s.queueFailed || s.tracksState == tracksFailed
			})
			p.mu.Lock()
			n := find(draw(t, p, s), "state-tracks-failed")
			p.mu.Unlock()
			if n == nil || n.Action == "" {
				t.Fatal("failed list has no recovery action")
			}
			fail.Store(false)
			p.event(nil, protocol.EventParams{Pane: "p1", Node: n.ID, Kind: protocol.KindPress})
			eventually(t, "the retried list", func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				if queue {
					return len(s.queue) == 2 && !s.queueFailed && s.tab == tabQueue
				}
				return len(s.tracks) == 2 && s.tracksState == tracksReady && s.drill == "album-a1"
			})
		})
	}
}

func TestOfflineLibraryRetriesWithoutLeavingItsTab(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	var fail atomic.Bool
	fail.Store(true)
	p.library = libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"a1","name":"One"}]}`))
	}))
	p.mu.Lock()
	s.tab, s.loaded = tabPlaylists, make(map[tabKind]bool)
	p.mu.Unlock()
	p.loadTab("p1", tabPlaylists)
	eventually(t, "the offline library", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libNetworkDown
	})
	p.mu.Lock()
	n := find(draw(t, p, s), "state-offline")
	p.mu.Unlock()
	if n == nil || n.Action == "" || n.Detail == "" {
		t.Fatal("offline library has no recovery instruction and action")
	}
	fail.Store(false)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: n.ID, Kind: protocol.KindPress})
	eventually(t, "the retried library", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libReady && s.tab == tabPlaylists && len(s.entries) == 1
	})
}

// The Up next tab lists the queue, reads it again when the track changes,
// and plays a pressed row.
func TestUpNext(t *testing.T) {
	var calls atomic.Int32
	p, s, eng, runner := drillPlugin(t)
	p.library = libraryClient(t, tracksHandler(&calls))
	p.mu.Lock()
	s.visible = true
	p.mu.Unlock()

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-queue", Kind: protocol.KindPress})
	eventually(t, "the queue", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(s.queue) == 2
	})
	p.mu.Lock()
	root := draw(t, p, s)
	p.mu.Unlock()
	if list := find(root, "queue"); list == nil || len(list.Items) != 2 || list.Items[0].Title != "Song t3" || list.Items[1].Leading != "2" {
		t.Fatalf("queue = %+v", list)
	}
	if find(root, "tab-queue") == nil || !find(root, "tab-queue").Active {
		t.Error("the Up next tab is not marked as open")
	}

	before := calls.Load()
	next := s.snap
	next.URI, next.Track = "spotify:track:other", "Other"
	next.ReadAt = time.Now()
	p.playback(next)
	eventually(t, "a second read of the queue", func() bool { return calls.Load() > before })

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "queue", Kind: protocol.KindSelect, Value: "q-1"})
	eventuallyScript(t, runner, `play track "spotify:track:t4"`)
	if len(eng.played()) != 0 {
		t.Fatalf("a queue row played inside a context: %v", eng.played())
	}
}

// Playing shows the tint; pausing or stopping sends none, so the colour of
// the pane and of the app fades out, and playing again brings it back.
func TestTheTintFadesOnPause(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	tinted := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return find(draw(t, p, s), "art").Tint
	}
	if !tinted() {
		t.Fatal("a playing track sends no tint")
	}
	for _, state := range []transport.State{transport.StatePaused, transport.StateStopped} {
		p.mu.Lock()
		s.snap.State = state
		p.mu.Unlock()
		if tinted() {
			t.Fatalf("a %s track still sends a tint", state)
		}
	}
	p.mu.Lock()
	s.snap.State = transport.StatePlaying
	p.mu.Unlock()
	if !tinted() {
		t.Fatal("playing again did not bring the tint back")
	}
	// The small and the medium players follow.
	for _, size := range []protocol.Size{{Width: 280, Height: 150}, {Width: 400, Height: 320}} {
		p.mu.Lock()
		s.size, s.snap.State = size, transport.StatePaused
		p.mu.Unlock()
		if tinted() {
			t.Fatalf("a paused track tints the %v player", size)
		}
	}
}

// Shuffle shows its new value at once and keeps it while Spotify pushes
// states that still have the old one, until Spotify reports the new one.
func TestShuffleKeepsItsGuessUntilSpotifyConfirms(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "shuffle", Kind: protocol.KindPress})
	p.mu.Lock()
	if !s.snap.Shuffle || s.shuffleGuess == nil {
		t.Fatalf("shuffle = %v, guess = %v", s.snap.Shuffle, s.shuffleGuess)
	}
	root := draw(t, p, s)
	p.mu.Unlock()
	if shuffle := find(root, "shuffle"); !shuffle.Active {
		t.Fatal("the shuffle button does not show it is on")
	}

	old := s.snap
	old.Shuffle, old.ReadAt = false, time.Now()
	p.playback(old)
	p.mu.Lock()
	held := s.snap.Shuffle
	p.mu.Unlock()
	if !held {
		t.Fatal("a state with the old shuffle took the guess back")
	}

	confirmed := old
	confirmed.Shuffle = true
	p.playback(confirmed)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !s.snap.Shuffle || s.shuffleGuess != nil {
		t.Fatalf("shuffle = %v, guess = %v after the confirmation", s.snap.Shuffle, s.shuffleGuess)
	}
}

// A repeat that no state confirms goes back when the guess times out.
func TestAnUnconfirmedRepeatGoesBack(t *testing.T) {
	var fire func()
	previous := afterFunc
	afterFunc = func(_ time.Duration, f func()) { fire = f }
	t.Cleanup(func() { afterFunc = previous })

	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.toggle("p1", "repeat")
	p.mu.Lock()
	on := s.snap.Repeat
	p.mu.Unlock()
	if !on || fire == nil {
		t.Fatalf("repeat = %v, timer = %v", on, fire != nil)
	}
	fire()
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Repeat || s.repeatGuess != nil {
		t.Fatal("the guess stood after its timeout")
	}
}

// Showing a tab again after a minute loads it once more; before that, it
// does not.
func TestAStaleTabLoadsAgainWhenShown(t *testing.T) {
	handler, playlists, _, _ := libraryCounts(t, nil)
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = libraryClient(t, handler)
	s.tab = tabPlaylists

	p.syncLibrary("p1")
	eventually(t, "the first load", func() bool { return playlists.Load() == 1 })
	eventually(t, "the load to end", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !s.loading
	})

	p.visible(nil, "p1", true)
	time.Sleep(50 * time.Millisecond)
	if got := playlists.Load(); got != 1 {
		t.Fatalf("a fresh tab loaded again: %d requests", got)
	}

	p.mu.Lock()
	s.loadedAt[tabPlaylists] = time.Now().Add(-2 * libraryFresh)
	p.mu.Unlock()
	p.visible(nil, "p1", false)
	p.visible(nil, "p1", true)
	eventually(t, "the second load", func() bool { return playlists.Load() == 2 })
}

// tileMin and tileMax are the widths the grid's tiles stay in where the pane
// has room for a whole count of them.
const tileMin, tileMax = 160, 200

// The grid fills its row with tiles of about 160 to 200 points: where no
// whole number of tiles lands in that range, the nearest one to 180 wins,
// never more than 25 points out. The library shows at most 48 of them.
func TestTheGridFitsTilesAndCapsThem(t *testing.T) {
	for width := 500; width <= 1600; width += 37 {
		tile, cols := gridFit(width)
		if tile < tileMin-25 || tile > tileMax+25 || cols < 2 {
			t.Errorf("width %d: %d tiles of %d", width, cols, tile)
		}
		if room := rowRoom(width); cols*tile+gridGap*(cols-1) > room {
			t.Errorf("width %d: %d tiles of %d overflow %d", width, cols, tile, room)
		}
	}
	if tile, cols := gridFit(1000); tile < tileMin || tile > tileMax {
		t.Errorf("1000 wide: %d tiles of %d, want 160 to 200", cols, tile)
	}
	if tile, cols := gridFit(300); cols != 2 || tile < 100 {
		t.Errorf("narrow pane: %d tiles of %d", cols, tile)
	}

	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	s.tab = tabAlbums
	for i := range 120 {
		s.entries = append(s.entries, entry{id: fmt.Sprintf("album-%d", i), title: "Album"})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	root := draw(t, p, s)
	if list := find(root, "results"); len(list.Items) != maxTiles+1 || list.Items[maxTiles].ID != moreID {
		t.Fatalf("%d tiles, want %d and Show More", len(list.Items), maxTiles)
	}
}

// Show More at the end of a capped grid adds another page of tiles, until
// every row shows and the tile is gone; another tab starts at one page.
func TestShowMoreRaisesTheCap(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = libraryClient(t, tracksHandler(nil))
	s.tab = tabAlbums
	for i := range 120 {
		s.entries = append(s.entries, entry{id: fmt.Sprintf("album-%d", i), title: "Album"})
	}
	shown := func() []protocol.Item {
		p.mu.Lock()
		defer p.mu.Unlock()
		return find(draw(t, p, s), "results").Items
	}
	press := func() {
		p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: moreID})
	}
	if items := shown(); len(items) != maxTiles+1 || items[len(items)-1].Title != locales.SpotifyShowMore() {
		t.Fatalf("first page: %d items", len(items))
	}
	press()
	if items := shown(); len(items) != 2*maxTiles+1 || items[2*maxTiles-1].ID != "album-95" {
		t.Fatalf("second page: %d items", len(items))
	}
	press()
	if items := shown(); len(items) != 120 || items[len(items)-1].ID != "album-119" {
		t.Fatalf("last page: %d items, want all 120 and no Show More", len(items))
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-playlists", Kind: protocol.KindPress})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.shownTiles != 0 {
		t.Fatalf("a new tab kept %d tiles", s.shownTiles)
	}
}

// A narrow pane gives the controls and the volume a row each.
func TestTheNarrowPlayerHasThreeRows(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 340, Height: 600}, modePlaying, libReady)
	p.mu.Lock()
	defer p.mu.Unlock()
	root := draw(t, p, s)
	for _, id := range []string{"head", "controls", "seek-row", "volume-row"} {
		if find(root, id) == nil {
			t.Errorf("the narrow player has no %q", id)
		}
	}
	if volume := find(root, "volume"); volume == nil || volume.Width != 0 {
		t.Fatalf("volume = %+v, want a slider that grows", volume)
	}
	if find(find(root, "head"), "play") != nil {
		t.Error("the narrow head still holds the controls")
	}
}

// Sign Out forgets the sign-in, keeps the Client ID, and shows every pane
// signed out, with no drill-in left.
func TestSignOut(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"})
	drillReady(t, p, s)
	p.mu.Lock()
	root := draw(t, p, s)
	p.mu.Unlock()
	if find(root, "sign-out") != nil {
		t.Fatal("the pane still draws Sign Out: it lives in Settings now")
	}

	p.mu.Lock()
	searches := s.searchSeq
	p.mu.Unlock()
	// The Sign Out button of Settings runs the plugin's sign-out command;
	// the host sent the Client ID with the settings before.
	p.mu.Lock()
	p.values[web.SettingClientID] = "client"
	p.mu.Unlock()
	p.command(nil, "sign-out")
	eventually(t, "the sign-out", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libSignedOut
	})
	if p.library.SignedIn() || p.library.ClientID() != "client" {
		t.Fatalf("signed in = %v, client = %q", p.library.SignedIn(), p.library.ClientID())
	}
	eventually(t, "the engine to stop", func() bool { return !p.pushes() })
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.searchSeq == searches {
		t.Error("a search in flight can still write its results after the sign-out")
	}
	if s.drill != "" || len(s.playlists) != 0 || len(s.albums) != 0 {
		t.Fatalf("drill = %q, lists left", s.drill)
	}
	if root := draw(t, p, s); find(root, "sign-in") == nil || find(root, "sign-out") != nil {
		t.Fatalf("the signed-out pane: %q", texts(root))
	}
}

// Trees are sent one at a time for a pane, in the order they are built.
func TestRendersOfAPaneDoNotOverlap(t *testing.T) {
	var inFlight, overlaps atomic.Int32
	previous := drawTree
	drawTree = func(*sdk.Conn, string, protocol.Tree) {
		if inFlight.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(time.Millisecond)
		inFlight.Add(-1)
	}
	t.Cleanup(func() { drawTree = previous })

	p, _, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.conn = &sdk.Conn{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				p.render("p1")
			}
		}()
	}
	wg.Wait()
	if overlaps.Load() != 0 {
		t.Fatalf("%d renders of one pane overlapped", overlaps.Load())
	}
}

// The mute button silences the player and a second press brings the volume
// back.
func TestMuteTogglesTheVolume(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "mute", Kind: protocol.KindPress})
	eventuallyScript(t, runner, "set sound volume to 0")
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "mute", Kind: protocol.KindPress})
	eventuallyScript(t, runner, "set sound volume to 70")
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Volume != 70 {
		t.Fatalf("volume = %d", s.snap.Volume)
	}
}

// A press on a row of Up next plays it, and the queue is read again once
// the player answers with that track: the pane guessed the URI already, so
// the answer does not look like a change of track unless the reload compares
// with the track before the guess.
func TestAPressOnAQueueRowReadsTheQueueAgain(t *testing.T) {
	var calls atomic.Int32
	p, s, _, runner := drillPlugin(t)
	p.library = libraryClient(t, tracksHandler(&calls))
	p.mu.Lock()
	s.visible = true
	p.mu.Unlock()
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-queue", Kind: protocol.KindPress})
	eventually(t, "the queue", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(s.queue) == 2
	})

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "queue", Kind: protocol.KindSelect, Value: "q-1"})
	eventuallyScript(t, runner, `play track "spotify:track:t4"`)
	before := calls.Load()
	answer := transport.Snapshot{
		State: transport.StatePlaying, Track: "Song t4", URI: "spotify:track:t4",
		Duration: time.Minute, Volume: 70, ReadAt: time.Now(),
	}
	p.playback(answer)
	eventually(t, "a read of the queue after the press", func() bool { return calls.Load() > before })
}

// A pane without room for the queue draws tiles and reads no queue.
func TestASmallPaneDoesNotReadTheQueue(t *testing.T) {
	var calls atomic.Int32
	p, s, _, _ := drillPlugin(t)
	p.library = libraryClient(t, tracksHandler(&calls))
	p.mu.Lock()
	s.visible, s.tab, s.size = true, tabQueue, protocol.Size{Width: 700, Height: 300}
	p.mu.Unlock()

	next := s.snap
	next.URI, next.Track = "spotify:track:other", "Other"
	next.ReadAt = time.Now()
	p.playback(next)
	p.syncLibrary("p1")
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d queue reads in a medium pane", n)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.tab != tabPlaylists {
		t.Fatalf("tab = %v, want the playlists in place of the queue", s.tab)
	}
}

// Signing out stops the engine even when forgetting the tokens failed.
func TestSignOutStopsTheEngineOnAFailure(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	raw, _ := json.Marshal(web.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Scope: "streaming", ExpiresAt: time.Now().Add(time.Hour)})
	p.library = web.New(web.Config{
		Secrets: failingSecrets{web.MapSecrets{web.TokensKey: string(raw)}},
		OpenURL: web.OpenURLFunc(func(string) error { return nil }),
	})
	p.library.SetClientID("client")
	p.syncEngine()
	wakeEngine(t, p, f)
	if f.started != 1 {
		t.Fatalf("engine started %d times", f.started)
	}

	p.signOut()
	eventually(t, "the engine to stop", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.closed == 1
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice != localeCommandFailed() {
		t.Fatalf("notice = %q, want the failure said", s.notice)
	}
}

// failingSecrets refuses to forget a secret, as a locked keychain does.
type failingSecrets struct{ web.MapSecrets }

func (failingSecrets) Set(string, string) error { return errors.New("locked") }

// The icon buttons carry the localized names, and a muted player shows the
// crossed speaker, switched on.
func TestIconButtonsHaveLabelsAndMuteShowsItself(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.mu.Lock()
	root := draw(t, p, s)
	p.mu.Unlock()
	for id, want := range map[string]string{
		"tab-search": locales.SpotifyTabSearch(), "tab-queue": locales.SpotifyTabQueue(),
		"shuffle": locales.SpotifyShuffle(), "repeat": locales.SpotifyRepeat(), "mute": locales.SpotifyVolume(),
	} {
		if n := find(root, id); n == nil || n.Label != want {
			t.Errorf("%s label = %+v, want %q", id, n, want)
		}
	}
	if n := find(root, "mute"); n.Icon != "volume" || n.Active {
		t.Errorf("sound on: icon %q active %v", n.Icon, n.Active)
	}

	p.mu.Lock()
	s.snap.Volume = 0
	root = draw(t, p, s)
	p.mu.Unlock()
	if n := find(root, "mute"); n.Icon != "volume-off" || !n.Active {
		t.Errorf("muted: icon %q active %v", n.Icon, n.Active)
	}
}
