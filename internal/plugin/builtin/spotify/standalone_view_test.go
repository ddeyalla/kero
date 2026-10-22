package spotify

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// A player with no track loaded draws no player block in any layout: the
// library has the whole pane. A track that is paused keeps the player, so
// its controls do not vanish.
func TestAPaneWithNoTrackLoadedDrawsTheLibraryAlone(t *testing.T) {
	sizes := map[string]protocol.Size{
		"small":  {Width: 280, Height: 150},
		"medium": {Width: 400, Height: 320},
		"large":  {Width: 700, Height: 600},
	}
	for name, size := range sizes {
		for _, mode := range []playbackMode{modeStarting, modeIdle} {
			t.Run(fmt.Sprintf("%s %d", name, mode), func(t *testing.T) {
				p, s, _ := testPlugin(size, mode, libReady)
				s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}
				s.tab = tabAlbums
				root := draw(t, p, s)
				for _, id := range []string{"state-idle", "state-starting", "head", "play", "art", "title", "progress", "seek"} {
					if find(root, id) != nil {
						t.Errorf("the idle %s pane draws %q", name, id)
					}
				}
				for _, id := range []string{"tab-playlists", "tab-albums", "results"} {
					if find(root, id) == nil {
						t.Errorf("the idle %s pane has no %q", name, id)
					}
				}
				if find(root, "sign-out") != nil {
					t.Errorf("the %s pane draws Sign Out", name)
				}
			})
		}

		t.Run(name+" paused", func(t *testing.T) {
			p, s, _ := testPlugin(size, modePlaying, libReady)
			s.snap.State = transport.StatePaused
			if find(draw(t, p, s), "play") == nil {
				t.Errorf("the paused %s pane lost its controls", name)
			}
		})

		t.Run(name+" last played", func(t *testing.T) {
			// The track played last, which only an idle player shows, is
			// no track loaded in this session.
			p, s, _ := testPlugin(size, modePlaying, libReady)
			s.showLast(&web.Track{Name: "Juicy", URI: "spotify:track:t1"}, transport.Snapshot{})
			if root := draw(t, p, s); find(root, "play") != nil || find(root, "title") != nil {
				t.Errorf("the %s pane shows the last played track as a player", name)
			}
		})
	}
}

// The first tree of a pane that opens signed in never asks to sign in: a
// pane draws nothing until the stored sign-in and the engine are known.
func TestTheFirstTreeOfASignedInOpenHasNoSignInGate(t *testing.T) {
	useFakeEngine(t, nil)
	oldBackend := newBackend
	newBackend = func(*sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
		return transport.New(&recordingRunner{}), silentSource{}, streamingLibrary(t, "streaming user-library-read")
	}
	t.Cleanup(func() { newBackend = oldBackend })

	var mu sync.Mutex
	var drawn []*protocol.Node
	oldDraw := drawTree
	drawTree = func(_ *sdk.Conn, _ string, tree protocol.Tree) {
		mu.Lock()
		drawn = append(drawn, tree.Root)
		mu.Unlock()
	}
	t.Cleanup(func() { drawTree = oldDraw })

	p := newPlugin()
	p.values[web.SettingClientID] = "client"
	conn := new(sdk.Conn)
	p.open(conn, sdk.OpenEvent{Pane: "p1", Widget: widgetPlayer, Size: protocol.Size{Width: 700, Height: 600}})
	// Events that arrive while the pane opens render it too.
	p.resize(conn, "p1", protocol.Size{Width: 700, Height: 600})
	p.visible(conn, "p1", true)
	time.Sleep(20 * time.Millisecond)
	// Loads still in flight draw nothing once the test is over.
	p.mu.Lock()
	p.conn = nil
	p.mu.Unlock()

	mu.Lock()
	defer mu.Unlock()
	if len(drawn) == 0 {
		t.Fatal("the pane drew nothing")
	}
	for i, root := range drawn {
		for _, id := range []string{"sign-in", "sign-in-again", "setup", "state-unavailable"} {
			if find(root, id) != nil {
				t.Errorf("tree %d of the open draws %q", i, id)
			}
		}
	}
	if find(drawn[len(drawn)-1], "tab-playlists") == nil {
		t.Errorf("the last tree is not the library: %q", texts(drawn[len(drawn)-1]))
	}
}

// A pane that is still opening draws an empty pane, whatever the sign-in
// looks like at that moment.
func TestAnOpeningPaneDrawsNoGate(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modeStarting, libSignedOut)
	p.engine = nil
	s.opening = true
	root := draw(t, p, s)
	if find(root, "sign-in") != nil || len(root.Children) != 0 {
		t.Fatalf("the opening pane draws %q", texts(root))
	}
	s.opening = false
	if find(draw(t, p, s), "sign-in") == nil {
		t.Fatal("a signed-out pane no longer asks to sign in")
	}
}

// The Sign Out button of Settings reaches a plugin that no pane opened: the
// command makes the backend, forgets the tokens and keeps the Client ID.
func TestTheSignOutCommandNeedsNoPane(t *testing.T) {
	var library *web.Client
	oldBackend := newBackend
	newBackend = func(*sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
		library = streamingLibrary(t, "streaming user-library-read")
		return transport.New(&recordingRunner{}), silentSource{}, library
	}
	t.Cleanup(func() { newBackend = oldBackend })

	p := newPlugin()
	p.values[web.SettingClientID] = "client"
	p.command(nil, "sign-out")
	eventually(t, "the sign-out", func() bool { return library != nil && !library.SignedIn() })
	if library.ClientID() != "client" {
		t.Fatalf("client ID = %q, want it kept", library.ClientID())
	}
}
