package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// seededSecrets returns secrets that hold one token.
func seededSecrets(t *testing.T, token Token) MapSecrets {
	t.Helper()
	raw, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	return MapSecrets{TokensKey: string(raw)}
}

// liveToken is a token that is valid at now.
func liveToken(now time.Time) Token {
	return Token{
		AccessToken:  "access",
		TokenType:    "Bearer",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(time.Hour),
	}
}

func TestSearch(t *testing.T) {
	now := testNow()
	var query = make(map[string][]string)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access" {
			t.Errorf("Authorization = %q", got)
		}
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"albums": {"items": [
				{"id": "a1", "uri": "spotify:album:a1", "name": "Discovery", "album_type": "album",
				 "release_date": "2001-03-12", "total_tracks": 14,
				 "artists": [{"name": "Daft Punk"}],
				 "images": [
					{"url": "https://i/640", "width": 640, "height": 640},
					{"url": "https://i/64", "width": 64, "height": 64},
					{"url": "https://i/300", "width": 300, "height": 300}
				 ]}
			]},
			"playlists": {"items": [
				null,
				{"id": "p1", "uri": "spotify:playlist:p1", "name": "Late Night", "description": "Slow jams",
				 "owner": {"display_name": "Ada", "id": "ada"}, "tracks": {"total": 42},
				 "images": [{"url": "https://i/p300", "width": 300}, {"url": "https://i/p60", "width": 60}]},
				{"id": "p2", "name": "No Owner"}
			]}
		}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	result, err := client.Search(context.Background(), "  daft punk  ")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := query["q"]; len(got) != 1 || got[0] != "daft punk" {
		t.Errorf("q = %v", got)
	}
	if got := query["type"]; len(got) != 1 || got[0] != "album,playlist" {
		t.Errorf("type = %v", got)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "10" {
		t.Errorf("limit = %v", got)
	}

	if len(result.Albums) != 1 {
		t.Fatalf("albums = %+v", result.Albums)
	}
	album := result.Albums[0]
	if album.ID != "a1" || album.Name != "Discovery" || album.AlbumType != "album" {
		t.Errorf("album = %+v", album)
	}
	if album.ReleaseDate != "2001-03-12" || album.TotalTracks != 14 {
		t.Errorf("album = %+v", album)
	}
	if album.ArtistLine() != "Daft Punk" {
		t.Errorf("ArtistLine = %q", album.ArtistLine())
	}
	if album.Artwork() != "https://i/64" {
		t.Errorf("Artwork = %q, want the smallest image", album.Artwork())
	}

	if len(result.Playlists) != 2 {
		t.Fatalf("playlists = %+v", result.Playlists)
	}
	playlist := result.Playlists[0]
	if playlist.ID != "p1" || playlist.Owner != "Ada" || playlist.TotalTracks != 42 {
		t.Errorf("playlist = %+v", playlist)
	}
	if playlist.Artwork() != "https://i/p60" {
		t.Errorf("playlist Artwork = %q", playlist.Artwork())
	}
	if second := result.Playlists[1]; second.ID != "p2" || second.Owner != "" || second.Artwork() != "" {
		t.Errorf("playlist without owner = %+v", second)
	}

	last, ok := client.LastSearch()
	if !ok || len(last.Albums) != 1 || last.Albums[0].ID != "a1" {
		t.Errorf("LastSearch = %+v, %v", last, ok)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer api.Close()
	now := testNow()
	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	result, err := client.Search(context.Background(), "   ")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Albums) != 0 || len(result.Playlists) != 0 {
		t.Errorf("result = %+v", result)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0", requests)
	}
}

func TestPlaylists(t *testing.T) {
	now := testNow()
	var query = make(map[string][]string)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/playlists" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		io.WriteString(w, `{"items": [
			{"id": "p1", "name": "One", "description": "First",
			 "owner": {"display_name": "Ada", "id": "ada"}, "tracks": {"total": 3},
			 "images": [{"url": "https://i/300", "width": 300}, {"url": "https://i/60", "width": 60}]},
			{"id": "p2", "name": "Two", "owner": {"id": "bob"}},
			null
		]}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	list, err := client.Playlists(context.Background())
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "50" {
		t.Errorf("limit = %v", got)
	}
	if len(list) != 2 {
		t.Fatalf("playlists = %+v", list)
	}
	if list[0].ID != "p1" || list[0].Owner != "Ada" || list[0].TotalTracks != 3 || list[0].Artwork() != "https://i/60" {
		t.Errorf("playlist = %+v", list[0])
	}
	if list[1].Owner != "bob" {
		t.Errorf("owner = %q, want the id when there is no name", list[1].Owner)
	}
	last, ok := client.LastPlaylists()
	if !ok || len(last) != 2 {
		t.Errorf("LastPlaylists = %+v, %v", last, ok)
	}
}

func TestAlbums(t *testing.T) {
	now := testNow()
	var query = make(map[string][]string)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/albums" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		io.WriteString(w, `{"items": [
			{"added_at": "2026-01-01T00:00:00Z", "album": {
				"id": "a1", "uri": "spotify:album:a1", "name": "Discovery", "album_type": "album",
				"release_date": "2001-03-12", "total_tracks": 14,
				"artists": [{"name": "Daft Punk"}, {"name": "Guest"}],
				"images": [{"url": "https://i/640", "width": 640}, {"url": "https://i/64", "width": 64}]
			}},
			{"album": null},
			null
		]}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	list, err := client.Albums(context.Background())
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "50" {
		t.Errorf("limit = %v", got)
	}
	if len(list) != 1 {
		t.Fatalf("albums = %+v", list)
	}
	album := list[0]
	if album.ID != "a1" || album.TotalTracks != 14 || album.Artwork() != "https://i/64" {
		t.Errorf("album = %+v", album)
	}
	if album.ArtistLine() != "Daft Punk, Guest" {
		t.Errorf("ArtistLine = %q", album.ArtistLine())
	}
	last, ok := client.LastAlbums()
	if !ok || len(last) != 1 {
		t.Errorf("LastAlbums = %+v, %v", last, ok)
	}
}

func TestRefreshBeforeExpiry(t *testing.T) {
	now := testNow()
	var form url.Values
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token": "new-access", "token_type": "Bearer", "expires_in": 3600}`)
	}))
	defer accounts.Close()

	var authorization string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		io.WriteString(w, `{"items": []}`)
	}))
	defer api.Close()

	secrets := seededSecrets(t, Token{
		AccessToken:  "old-access",
		RefreshToken: "the-refresh",
		ExpiresAt:    now.Add(30 * time.Second),
	})
	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		Secrets:     secrets,
		Now:         func() time.Time { return now },
	})
	if _, err := client.Playlists(context.Background()); err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if got := form.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q", got)
	}
	if got := form.Get("refresh_token"); got != "the-refresh" {
		t.Errorf("refresh_token = %q", got)
	}
	if got := form.Get("client_id"); got != "client-id" {
		t.Errorf("client_id = %q", got)
	}
	if authorization != "Bearer new-access" {
		t.Errorf("Authorization = %q", authorization)
	}

	var saved Token
	if err := json.Unmarshal([]byte(secrets[TokensKey]), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.AccessToken != "new-access" {
		t.Errorf("saved access token = %q", saved.AccessToken)
	}
	if saved.RefreshToken != "the-refresh" {
		t.Errorf("saved refresh token = %q, want the old one", saved.RefreshToken)
	}
}

func TestNoRefreshWhileValid(t *testing.T) {
	now := testNow()
	var refreshes int
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes++
	}))
	defer accounts.Close()

	var authorization string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		io.WriteString(w, `{"items": []}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		Secrets:     seededSecrets(t, liveToken(now)),
		Now:         func() time.Time { return now },
	})
	if _, err := client.Playlists(context.Background()); err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if refreshes != 0 {
		t.Errorf("refreshes = %d, want 0", refreshes)
	}
	if authorization != "Bearer access" {
		t.Errorf("Authorization = %q", authorization)
	}
}

func TestRefusedTokenIsRefreshedOnce(t *testing.T) {
	now := testNow()
	var refreshes int
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes++
		io.WriteString(w, `{"access_token": "fresh", "expires_in": 3600}`)
	}))
	defer accounts.Close()

	var requests int
	var authorizations []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		if requests == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error": {"status": 401, "message": "The access token expired"}}`)
			return
		}
		io.WriteString(w, `{"items": [{"id": "p1", "name": "One"}]}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		Secrets:     seededSecrets(t, liveToken(now)),
		Now:         func() time.Time { return now },
	})
	list, err := client.Playlists(context.Background())
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2", requests)
	}
	if refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", refreshes)
	}
	if len(authorizations) != 2 || authorizations[0] != "Bearer access" || authorizations[1] != "Bearer fresh" {
		t.Errorf("Authorizations = %v", authorizations)
	}
	if len(list) != 1 || list[0].ID != "p1" {
		t.Errorf("playlists = %+v", list)
	}
}

func TestTokenRefusedAfterRefresh(t *testing.T) {
	now := testNow()
	var refreshes int
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes++
		io.WriteString(w, `{"access_token": "fresh", "expires_in": 3600}`)
	}))
	defer accounts.Close()

	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error": {"status": 401, "message": "The access token expired"}}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		Secrets:     seededSecrets(t, liveToken(now)),
		Now:         func() time.Time { return now },
	})
	_, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrTokenRefused) {
		t.Fatalf("Playlists = %v, want ErrTokenRefused", err)
	}
	if got := State(err); got != StateTokenRefused {
		t.Errorf("State = %q", got)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2", requests)
	}
	if refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", refreshes)
	}
}

func TestRefreshRefused(t *testing.T) {
	now := testNow()
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error": "invalid_grant", "error_description": "Refresh token revoked"}`)
	}))
	defer accounts.Close()

	var requests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer api.Close()

	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		Secrets: seededSecrets(t, Token{
			AccessToken:  "old",
			RefreshToken: "revoked",
			ExpiresAt:    now.Add(-time.Minute),
		}),
		Now: func() time.Time { return now },
	})
	_, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrTokenRefused) {
		t.Fatalf("Playlists = %v, want ErrTokenRefused", err)
	}
	if got := State(err); got != StateTokenRefused {
		t.Errorf("State = %q", got)
	}
	if requests != 0 {
		t.Errorf("API requests = %d, want 0", requests)
	}
}

func TestPremiumLapsed(t *testing.T) {
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error": {"status": 403, "message": "Player command failed: Premium required"}}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	_, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrPremiumLapsed) {
		t.Fatalf("Playlists = %v, want ErrPremiumLapsed", err)
	}
	if got := State(err); got != StatePremiumLapsed {
		t.Errorf("State = %q", got)
	}
}

func TestForbiddenWithoutPremium(t *testing.T) {
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error": {"status": 403, "message": "Forbidden"}}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	_, err := client.Albums(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("Albums = %v, want a forbidden error", err)
	}
	if errors.Is(err, ErrTokenRefused) {
		t.Errorf("Albums = %v, must not be ErrTokenRefused", err)
	}
	if got := State(err); got != StateFailed {
		t.Errorf("State = %q, want StateFailed", got)
	}
}

func TestRefreshNetworkDown(t *testing.T) {
	now := testNow()
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer accounts.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called when the refresh fails")
	}))
	defer api.Close()

	client := New(Config{
		ClientID:    "client-id",
		BaseURL:     api.URL,
		AccountsURL: accounts.URL,
		Secrets: seededSecrets(t, Token{
			AccessToken:  "old",
			RefreshToken: "refresh",
			ExpiresAt:    now.Add(-time.Minute),
		}),
		Now: func() time.Time { return now },
	})
	_, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Playlists = %v, want ErrNetworkDown", err)
	}
	if got := State(err); got != StateNetworkDown {
		t.Errorf("State = %q", got)
	}
}

// flakyTransport fails every request while down is set.
type flakyTransport struct {
	base http.RoundTripper
	down atomic.Bool
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.down.Load() {
		return nil, errors.New("dial tcp 127.0.0.1:9090: connect: connection refused")
	}
	return f.base.RoundTrip(req)
}

func TestNetworkDownKeepsLastResults(t *testing.T) {
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			io.WriteString(w, `{"albums": {"items": [{"id": "a1", "name": "Discovery"}]}}`)
		case "/v1/me/playlists":
			io.WriteString(w, `{"items": [{"id": "p1", "name": "One"}]}`)
		case "/v1/me/albums":
			io.WriteString(w, `{"items": [{"album": {"id": "a1", "name": "Discovery"}}]}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer api.Close()

	transport := &flakyTransport{base: http.DefaultTransport}
	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		HTTP:     &http.Client{Transport: transport},
		Now:      func() time.Time { return now },
	})

	search, err := client.Search(context.Background(), "daft punk")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	playlists, err := client.Playlists(context.Background())
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	albums, err := client.Albums(context.Background())
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}

	transport.down.Store(true)
	gotSearch, err := client.Search(context.Background(), "daft punk")
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Search = %v, want ErrNetworkDown", err)
	}
	if len(gotSearch.Albums) != 1 || gotSearch.Albums[0].ID != search.Albums[0].ID {
		t.Errorf("search results were not kept: %+v", gotSearch)
	}
	gotPlaylists, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Playlists = %v, want ErrNetworkDown", err)
	}
	if len(gotPlaylists) != 1 || gotPlaylists[0].ID != playlists[0].ID {
		t.Errorf("playlists were not kept: %+v", gotPlaylists)
	}
	gotAlbums, err := client.Albums(context.Background())
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Albums = %v, want ErrNetworkDown", err)
	}
	if len(gotAlbums) != 1 || gotAlbums[0].ID != albums[0].ID {
		t.Errorf("albums were not kept: %+v", gotAlbums)
	}
}

func TestStates(t *testing.T) {
	now := testNow()

	noClient := New(Config{Secrets: MapSecrets{}})
	if got := noClient.Current(); got != StateNoClientID {
		t.Errorf("Current = %q", got)
	}
	if _, err := noClient.AccessToken(context.Background()); !errors.Is(err, ErrNoClientID) {
		t.Errorf("AccessToken = %v", err)
	}
	if _, err := noClient.Search(context.Background(), "x"); !errors.Is(err, ErrNoClientID) {
		t.Errorf("Search = %v", err)
	}
	if got := State(ErrNoClientID); got != StateNoClientID {
		t.Errorf("State = %q", got)
	}

	signedOut := New(Config{ClientID: "client-id", Secrets: MapSecrets{}})
	if got := signedOut.Current(); got != StateSignedOut {
		t.Errorf("Current = %q", got)
	}
	if _, err := signedOut.Playlists(context.Background()); !errors.Is(err, ErrSignedOut) {
		t.Errorf("Playlists = %v", err)
	}
	if got := State(ErrSignedOut); got != StateSignedOut {
		t.Errorf("State = %q", got)
	}

	ready := New(Config{ClientID: "client-id", Secrets: seededSecrets(t, liveToken(now)), Now: func() time.Time { return now }})
	if got := ready.Current(); got != StateReady {
		t.Errorf("Current = %q", got)
	}
	if got := State(nil); got != StateReady {
		t.Errorf("State(nil) = %q", got)
	}
	if got := State(errors.New("something else")); got != StateFailed {
		t.Errorf("State(other) = %q", got)
	}
}

func TestSignOut(t *testing.T) {
	now := testNow()
	secrets := seededSecrets(t, liveToken(now))
	client := New(Config{ClientID: "client-id", Secrets: secrets, Now: func() time.Time { return now }})
	if !client.SignedIn() {
		t.Fatal("SignedIn = false")
	}
	if err := client.SignOut(); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if client.SignedIn() {
		t.Error("SignedIn = true after SignOut")
	}
	if got := client.Current(); got != StateSignedOut {
		t.Errorf("Current = %q", got)
	}
	if _, ok := secrets[TokensKey]; ok {
		t.Error("the tokens secret is still set")
	}
}

func TestSetClientID(t *testing.T) {
	client := New(Config{Secrets: MapSecrets{}})
	if got := client.Current(); got != StateNoClientID {
		t.Errorf("Current = %q", got)
	}
	client.SetClientID(" client-id ")
	if got := client.ClientID(); got != "client-id" {
		t.Errorf("ClientID = %q", got)
	}
	if got := client.Current(); got != StateSignedOut {
		t.Errorf("Current = %q", got)
	}
}

func TestClientTimeoutIsNetworkDown(t *testing.T) {
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		io.WriteString(w, `{"items": []}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
		HTTP:     &http.Client{Timeout: 20 * time.Millisecond},
	})
	_, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Playlists = %v, want ErrNetworkDown", err)
	}
	if got := State(err); got != StateNetworkDown {
		t.Errorf("State = %q, want StateNetworkDown", got)
	}
}

func TestCanceledRequestIsNotNetworkDown(t *testing.T) {
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not answer a canceled request")
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Playlists(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Playlists = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrNetworkDown) {
		t.Errorf("Playlists = %v, must not be ErrNetworkDown", err)
	}
	if got := State(err); got != StateFailed {
		t.Errorf("State = %q, want StateFailed", got)
	}
}

func TestRateLimited(t *testing.T) {
	now := testNow()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error": {"status": 429, "message": "API rate limit exceeded"}}`)
	}))
	defer api.Close()

	client := New(Config{
		ClientID: "client-id",
		BaseURL:  api.URL,
		Secrets:  seededSecrets(t, liveToken(now)),
		Now:      func() time.Time { return now },
	})
	_, err := client.Playlists(context.Background())
	if !errors.Is(err, ErrNetworkDown) {
		t.Fatalf("Playlists = %v, want ErrNetworkDown", err)
	}
	if got := State(err); got != StateNetworkDown {
		t.Errorf("State = %q, want StateNetworkDown", got)
	}
	if message := err.Error(); !strings.Contains(strings.ToLower(message), "rate limit") {
		t.Errorf("Playlists = %q, want it to mention the rate limit", message)
	}
}

func TestStateReadsDoNotWaitForRefresh(t *testing.T) {
	now := testNow()
	var refreshes int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshes, 1)
		once.Do(func() { close(started) })
		<-release
		io.WriteString(w, `{"access_token": "fresh", "expires_in": 3600}`)
	}))
	defer accounts.Close()

	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		Secrets: seededSecrets(t, Token{
			AccessToken:  "stale",
			RefreshToken: "refresh",
			ExpiresAt:    now.Add(-time.Minute),
		}),
		Now: func() time.Time { return now },
	})

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := client.AccessToken(context.Background())
			errs <- err
		}()
	}
	<-started

	stateDone := make(chan struct{})
	go func() {
		defer close(stateDone)
		if !client.SignedIn() {
			t.Error("SignedIn = false during a refresh")
		}
		if got := client.Current(); got != StateReady {
			t.Errorf("Current = %q during a refresh", got)
		}
	}()
	select {
	case <-stateDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a state read waited for the token request")
	}

	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("AccessToken: %v", err)
		}
	}
	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Errorf("refreshes = %d, want 1", got)
	}
}

func TestStaleRefreshDoesNotReplaceNewerTokens(t *testing.T) {
	now := testNow()
	started := make(chan struct{})
	release := make(chan struct{})
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, `{"access_token": "refreshed", "expires_in": 3600}`)
	}))
	defer accounts.Close()

	secrets := seededSecrets(t, Token{
		AccessToken:  "stale",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(-time.Minute),
	})
	client := New(Config{
		ClientID:    "client-id",
		AccountsURL: accounts.URL,
		Secrets:     secrets,
		Now:         func() time.Time { return now },
	})
	got := make(chan string, 1)
	go func() {
		token, _ := client.AccessToken(context.Background())
		got <- token
	}()
	<-started

	newer := Token{AccessToken: "signed-in", RefreshToken: "new-refresh", ExpiresAt: now.Add(time.Hour)}
	if err := client.adoptToken(&newer); err != nil {
		t.Fatal(err)
	}
	close(release)

	if token := <-got; token != "signed-in" {
		t.Errorf("AccessToken = %q, want the newer token", token)
	}
	if client.SignedIn() == false {
		t.Error("SignedIn = false")
	}
}

func TestLateRefreshAdoptionIsDropped(t *testing.T) {
	now := testNow()
	secrets := seededSecrets(t, Token{
		AccessToken:  "old",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(-time.Minute),
	})
	client := New(Config{
		ClientID: "client-id",
		Secrets:  secrets,
		Now:      func() time.Time { return now },
	})
	old, err := client.tokens()
	if err != nil {
		t.Fatal(err)
	}

	// A sign-in lands while the refresh is on the wire.
	newer := Token{AccessToken: "signed-in", RefreshToken: "new-refresh", ExpiresAt: now.Add(time.Hour)}
	if err := client.adoptToken(&newer); err != nil {
		t.Fatal(err)
	}

	// The refresh then tries to adopt what it got from the old grant.
	late := Token{AccessToken: "refreshed", RefreshToken: "refresh", ExpiresAt: now.Add(time.Hour)}
	inUse, err := client.adoptTokenIf(old, &late)
	if err != nil {
		t.Fatal(err)
	}
	if inUse == nil || inUse.AccessToken != "signed-in" {
		t.Fatalf("adoptTokenIf = %+v, want the newer token", inUse)
	}
	current, err := client.tokens()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.AccessToken != "signed-in" {
		t.Fatalf("current = %+v, want the newer token", current)
	}
	var stored Token
	if err := json.Unmarshal([]byte(secrets[TokensKey]), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "signed-in" {
		t.Errorf("stored token = %q, want the newer token", stored.AccessToken)
	}
}

func TestSpotifyMessage(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"object", `{"error": {"status": 401, "message": "The access token expired"}}`, "The access token expired"},
		{"code", `{"error": "invalid_grant"}`, "invalid_grant"},
		{"description", `{"error": "invalid_grant", "error_description": "code expired"}`, "invalid_grant: code expired"},
		{"plain", "not json", "not json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := spotifyMessage([]byte(c.raw)); got != c.want {
				t.Errorf("spotifyMessage = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSmallImage(t *testing.T) {
	if got := SmallImage(nil); got != "" {
		t.Errorf("SmallImage(nil) = %q", got)
	}
	images := []Image{{URL: "big", Width: 640}, {URL: "small", Width: 64}, {URL: "mid", Width: 300}}
	if got := SmallImage(images); got != "small" {
		t.Errorf("SmallImage = %q", got)
	}
}

func TestClip(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := clip(long)
	if utf8.RuneCountInString(got) > 201 || !strings.HasSuffix(got, "…") {
		t.Errorf("clip = %q", got)
	}
	if clipped := clip("  two\nlines  "); clipped != "two lines" {
		t.Errorf("clip = %q", clipped)
	}
}
