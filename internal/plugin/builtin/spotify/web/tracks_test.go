package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tracksClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	now := testNow()
	return New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
}

func trackBody(id string) string {
	return fmt.Sprintf(`{"id":%q,"uri":"spotify:track:%s","name":"T %s","type":"track","duration_ms":1000,
		"artists":[{"name":"A"},{"name":"B"}],
		"album":{"name":"Alb","images":[{"url":"https://i/640","width":640},{"url":"https://i/64","width":64}]}}`, id, id, id)
}

func TestPlaylistTracksPagesAndSkips(t *testing.T) {
	var queries []string
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/playlists/p1/items" {
			t.Errorf("path = %q", r.URL.Path)
		}
		queries = append(queries, r.URL.RawQuery)
		q := r.URL.Query()
		if q.Get("market") != "from_token" || q.Get("limit") != "50" || q.Get("fields") == "" {
			t.Errorf("query = %v", q)
		}
		if q.Get("offset") == "0" {
			io.WriteString(w, `{"next":"https://x/next","items":[
				null,
				{"item":null},
				{"item":`+trackBody("a")+`},
				{"is_local":true,"item":{"id":"","uri":"spotify:local:x","name":"L","type":"track","is_local":true}},
				{"item":{"id":"e1","uri":"spotify:episode:e1","name":"Ep","type":"episode"}},
				{"item":{"id":"u","uri":"spotify:track:u","name":"U","type":"track","is_playable":false}}
			]}`)
			return
		}
		// The old field name still parses.
		io.WriteString(w, `{"next":null,"items":[{"track":`+trackBody("b")+`}]}`)
	})
	tracks, err := client.PlaylistTracks(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("requests = %v", queries)
	}
	if len(tracks) != 2 || tracks[0].ID != "a" || tracks[1].ID != "b" {
		t.Fatalf("tracks = %+v", tracks)
	}
	got := tracks[0]
	if got.URI != "spotify:track:a" || got.Name != "T a" || got.Album != "Alb" || got.DurationMS != 1000 ||
		got.ArtistLine() != "A, B" || got.Artwork != "https://i/64" {
		t.Errorf("track = %+v", got)
	}
	if cached, ok := client.CachedPlaylistTracks("p1"); !ok || len(cached) != 2 {
		t.Errorf("cached = %v %v", cached, ok)
	}
}

func TestPlaylistTracksFallsBackToTracks(t *testing.T) {
	var paths []string
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/items") {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"Not found"}}`)
			return
		}
		io.WriteString(w, `{"items":[{"track":`+trackBody("a")+`}]}`)
	})
	tracks, err := client.PlaylistTracks(context.Background(), "p1")
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %+v, err = %v", tracks, err)
	}
	if len(paths) != 2 || paths[1] != "/v1/playlists/p1/tracks" {
		t.Errorf("paths = %v", paths)
	}
}

func TestPlaylistTracksNotFoundStaysAnError(t *testing.T) {
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"message":"Not found"}}`)
	})
	if _, err := client.PlaylistTracks(context.Background(), "p1"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}

func TestPlaylistTracksCap(t *testing.T) {
	var calls atomic.Int32
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var items []string
		for i := range 50 {
			items = append(items, `{"item":`+trackBody(fmt.Sprintf("%s-%d", r.URL.Query().Get("offset"), i))+`}`)
		}
		io.WriteString(w, `{"next":"more","items":[`+strings.Join(items, ",")+`]}`)
	})
	tracks, err := client.PlaylistTracks(context.Background(), "big")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != maxTracks || calls.Load() != int32(maxTracks/trackPageSize) {
		t.Errorf("tracks = %d, calls = %d", len(tracks), calls.Load())
	}
}

func TestAlbumTracks(t *testing.T) {
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/albums/al1/tracks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `{"next":null,"items":[
			null,
			{"id":"t1","uri":"spotify:track:t1","name":"One","duration_ms":5,"artists":[{"name":"X"}]},
			{"id":"t2","uri":"spotify:track:t2","name":"Two","is_playable":false},
			{"id":"t3","uri":"spotify:local:t3","name":"Local","is_local":true}
		]}`)
	})
	tracks, err := client.AlbumTracks(context.Background(), "al1", "https://i/cover")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].ID != "t1" || tracks[0].Artwork != "https://i/cover" || tracks[0].ArtistLine() != "X" {
		t.Errorf("tracks = %+v", tracks)
	}
}

func TestQueue(t *testing.T) {
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/player/queue" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `{"currently_playing":`+trackBody("now")+`,"queue":[
			`+trackBody("n1")+`,
			null,
			{"id":"e","uri":"spotify:episode:e","name":"Ep","type":"episode"},
			`+trackBody("n2")+`]}`)
	})
	current, queue, err := client.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.ID != "now" || len(queue) != 2 || queue[0].ID != "n1" || queue[1].ID != "n2" {
		t.Errorf("current = %+v, queue = %+v", current, queue)
	}
}

func TestQueueNothingPlaying(t *testing.T) {
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"currently_playing":null,"queue":[]}`)
	})
	current, queue, err := client.Queue(context.Background())
	if err != nil || current != nil || len(queue) != 0 {
		t.Errorf("current = %+v, queue = %+v, err = %v", current, queue, err)
	}
}

func TestTrackErrors(t *testing.T) {
	status := http.StatusForbidden
	body := `{"error":{"message":"Insufficient client scope"}}`
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
	_, err := client.PlaylistTracks(context.Background(), "p1")
	if err == nil || !strings.Contains(err.Error(), "Insufficient client scope") || State(err) != StateFailed || !errors.Is(err, ErrForbidden) {
		t.Errorf("403: err = %v", err)
	}
	body = `{"error":{"message":"Premium required"}}`
	if _, _, err := client.Queue(context.Background()); !errors.Is(err, ErrPremiumLapsed) {
		t.Errorf("premium: err = %v", err)
	}
	status = http.StatusBadGateway
	if _, err := client.AlbumTracks(context.Background(), "a", ""); !errors.Is(err, ErrNetworkDown) {
		t.Errorf("502: err = %v", err)
	}
}

func TestTracksKeptOnNetworkFailure(t *testing.T) {
	fail := false
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		io.WriteString(w, `{"items":[{"item":`+trackBody("a")+`}]}`)
	})
	if _, err := client.PlaylistTracks(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	fail = true
	tracks, err := client.PlaylistTracks(context.Background(), "p1")
	if !errors.Is(err, ErrNetworkDown) || len(tracks) != 1 {
		t.Errorf("tracks = %+v, err = %v", tracks, err)
	}
}

func TestTrackCacheBoundAndReset(t *testing.T) {
	client := tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"items":[{"item":`+trackBody("a")+`}]}`)
	})
	for i := range trackListCacheSize + 3 {
		if _, err := client.PlaylistTracks(context.Background(), fmt.Sprintf("p%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.trackLists) != trackListCacheSize {
		t.Fatalf("kept %d lists", len(client.trackLists))
	}
	if _, ok := client.CachedPlaylistTracks("p0"); ok {
		t.Error("the oldest list was kept")
	}
	if _, ok := client.CachedPlaylistTracks(fmt.Sprintf("p%d", trackListCacheSize+2)); !ok {
		t.Error("the newest list was dropped")
	}
	if err := client.SignOut(); err != nil {
		t.Fatal(err)
	}
	if len(client.trackLists) != 0 {
		t.Error("SignOut kept the track lists")
	}
}

func TestTrackListNotKeptAfterSignOut(t *testing.T) {
	var client *Client
	client = tracksClient(t, func(w http.ResponseWriter, r *http.Request) {
		client.resetCache()
		io.WriteString(w, `{"items":[{"item":`+trackBody("a")+`}]}`)
	})
	if _, err := client.PlaylistTracks(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.CachedPlaylistTracks("p1"); ok {
		t.Error("a list from the old generation was kept")
	}
}

func TestScopesForTracks(t *testing.T) {
	for _, scope := range []string{"playlist-read-private", ScopeCollaborative, "user-read-playback-state"} {
		if !slices.Contains(DefaultScopes, scope) {
			t.Errorf("DefaultScopes lacks %s", scope)
		}
	}
}
