package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// maxResponse caps the body of one Web API answer.
const maxResponse = 8 << 20

// errTokenStale is the API refusing the access token. The client
// refreshes the token and tries once more.
var errTokenStale = errors.New("spotify: the token was refused")

// Image is one of the covers Spotify gives for an album or a playlist.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Album is one result of a search or of the saved albums.
type Album struct {
	ID          string
	URI         string
	Name        string
	AlbumType   string
	ReleaseDate string
	TotalTracks int
	Artists     []string
	Images      []Image
}

// Artwork is the smallest cover URL, which the list shows, or "" when
// Spotify gave no cover.
func (a Album) Artwork() string { return SmallImage(a.Images) }

// ArtistLine joins the artists for the second line of a result.
func (a Album) ArtistLine() string { return strings.Join(a.Artists, ", ") }

// Playlist is one result of a search or one of the user's playlists.
type Playlist struct {
	ID          string
	URI         string
	Name        string
	Description string
	Owner       string
	TotalTracks int
	Images      []Image
}

// Artwork is the smallest cover URL, or "" when Spotify gave none.
func (p Playlist) Artwork() string { return SmallImage(p.Images) }

// SearchResult is what a search found.
type SearchResult struct {
	Albums    []Album
	Playlists []Playlist
}

// SmallImage returns the URL of the smallest image, which is what a list
// row shows. It returns "" when there are no images.
func SmallImage(images []Image) string {
	if len(images) == 0 {
		return ""
	}
	small := images[0]
	for _, img := range images[1:] {
		if img.Width < small.Width {
			small = img
		}
	}
	return small.URL
}

// Search finds albums and playlists. An empty query finds nothing and
// sends no request.
//
// When the network is down, Search returns the last results together
// with ErrNetworkDown, so the pane can keep them and add a note.
func (c *Client) Search(ctx context.Context, query string) (SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return SearchResult{}, nil
	}
	params := url.Values{}
	params.Set("q", q)
	params.Set("type", "album,playlist")
	params.Set("limit", "10")
	var body struct {
		Albums struct {
			Items []albumJSON `json:"items"`
		} `json:"albums"`
		Playlists struct {
			Items []*playlistJSON `json:"items"`
		} `json:"playlists"`
	}
	if err := c.get(ctx, "/v1/search", params, &body); err != nil {
		if errors.Is(err, ErrNetworkDown) {
			if last, ok := c.lastSearchResults(); ok {
				return last, err
			}
		}
		return SearchResult{}, err
	}
	var result SearchResult
	for _, item := range body.Albums.Items {
		if album := item.album(); album.ID != "" {
			result.Albums = append(result.Albums, album)
		}
	}
	for _, item := range body.Playlists.Items {
		if item == nil {
			continue
		}
		if playlist := item.playlist(); playlist.ID != "" {
			result.Playlists = append(result.Playlists, playlist)
		}
	}
	c.cacheMu.Lock()
	c.lastSearch = result
	c.lastSearchSet = true
	c.cacheMu.Unlock()
	return result, nil
}

// Playlists returns the user's playlists, up to the first 50.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	params := url.Values{}
	params.Set("limit", "50")
	var body struct {
		Items []*playlistJSON `json:"items"`
	}
	if err := c.get(ctx, "/v1/me/playlists", params, &body); err != nil {
		if errors.Is(err, ErrNetworkDown) {
			if last, ok := c.lastPlaylistsResults(); ok {
				return last, err
			}
		}
		return nil, err
	}
	list := make([]Playlist, 0, len(body.Items))
	for _, item := range body.Items {
		if item == nil {
			continue
		}
		list = append(list, item.playlist())
	}
	c.cacheMu.Lock()
	c.lastPlaylists = list
	c.lastPlaylistsSet = true
	c.cacheMu.Unlock()
	return list, nil
}

// Albums returns the user's saved albums, up to the first 50.
func (c *Client) Albums(ctx context.Context) ([]Album, error) {
	params := url.Values{}
	params.Set("limit", "50")
	var body struct {
		Items []*struct {
			Album *albumJSON `json:"album"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/v1/me/albums", params, &body); err != nil {
		if errors.Is(err, ErrNetworkDown) {
			if last, ok := c.lastAlbumsResults(); ok {
				return last, err
			}
		}
		return nil, err
	}
	list := make([]Album, 0, len(body.Items))
	for _, item := range body.Items {
		if item == nil || item.Album == nil {
			continue
		}
		list = append(list, item.Album.album())
	}
	c.cacheMu.Lock()
	c.lastAlbums = list
	c.lastAlbumsSet = true
	c.cacheMu.Unlock()
	return list, nil
}

// LastSearch is the last search result, and whether a call got one.
func (c *Client) LastSearch() (SearchResult, bool) { return c.lastSearchResults() }

// LastPlaylists is the last playlist list, and whether a call got one.
func (c *Client) LastPlaylists() ([]Playlist, bool) { return c.lastPlaylistsResults() }

// LastAlbums is the last saved album list, and whether a call got one.
func (c *Client) LastAlbums() ([]Album, bool) { return c.lastAlbumsResults() }

func (c *Client) lastSearchResults() (SearchResult, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return c.lastSearch, c.lastSearchSet
}

func (c *Client) lastPlaylistsResults() ([]Playlist, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return c.lastPlaylists, c.lastPlaylistsSet
}

func (c *Client) lastAlbumsResults() ([]Album, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return c.lastAlbums, c.lastAlbumsSet
}

// get sends one GET with the access token. A refused token is refreshed
// once: one stale token must not look like a revoked sign-in.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	err = c.doGet(ctx, path, params, token, out)
	if errors.Is(err, errTokenStale) {
		token, err = c.refreshRefused(ctx)
		if err != nil {
			return err
		}
		err = c.doGet(ctx, path, params, token, out)
	}
	if errors.Is(err, errTokenStale) {
		return fmt.Errorf("%w: the API refused a fresh token", ErrTokenRefused)
	}
	return err
}

func (c *Client) doGet(ctx context.Context, path string, params url.Values, token string, out any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return networkError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return networkError(err)
	}
	if len(raw) > maxResponse {
		return fmt.Errorf("spotify: the response is too large")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errTokenStale
	case resp.StatusCode == http.StatusForbidden:
		message := spotifyMessage(raw)
		if premiumProblem(message) {
			return fmt.Errorf("%w: %s", ErrPremiumLapsed, message)
		}
		return fmt.Errorf("%w: %s", ErrTokenRefused, message)
	case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d: %s", ErrNetworkDown, resp.StatusCode, spotifyMessage(raw))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("spotify: request failed (HTTP %d): %s", resp.StatusCode, spotifyMessage(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("spotify: response: %v", err)
	}
	return nil
}

// premiumProblem tells whether an API message says the user needs
// Premium, which a development-mode app owner does.
func premiumProblem(message string) bool {
	return strings.Contains(strings.ToLower(message), "premium")
}

// spotifyMessage reads the message of an API error, which is either
// {"error":{"message":"..."}} or {"error":"code"}.
func spotifyMessage(raw []byte) string {
	var body struct {
		Error            json.RawMessage `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return clip(string(raw))
	}
	var message string
	if len(body.Error) > 0 {
		var code string
		if json.Unmarshal(body.Error, &code) == nil {
			message = code
		} else {
			var withMessage struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(body.Error, &withMessage) == nil {
				message = withMessage.Message
			}
		}
	}
	if body.ErrorDescription != "" {
		if message == "" {
			message = body.ErrorDescription
		} else {
			message += ": " + body.ErrorDescription
		}
	}
	if message == "" {
		message = string(raw)
	}
	return clip(message)
}

// clip shortens a message so that an error stays one line.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if utf8.RuneCountInString(s) > max {
		return string([]rune(s)[:max]) + "…"
	}
	return s
}

// albumJSON is an album as the API writes it.
type albumJSON struct {
	ID          string `json:"id"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	AlbumType   string `json:"album_type"`
	ReleaseDate string `json:"release_date"`
	TotalTracks int    `json:"total_tracks"`
	Artists     []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Images []Image `json:"images"`
}

func (a albumJSON) album() Album {
	album := Album{
		ID:          a.ID,
		URI:         a.URI,
		Name:        a.Name,
		AlbumType:   a.AlbumType,
		ReleaseDate: a.ReleaseDate,
		TotalTracks: a.TotalTracks,
		Images:      a.Images,
	}
	for _, artist := range a.Artists {
		if artist.Name != "" {
			album.Artists = append(album.Artists, artist.Name)
		}
	}
	return album
}

// playlistJSON is a playlist as the API writes it. The owner and the
// tracks can be missing from a search result.
type playlistJSON struct {
	ID          string `json:"id"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       *struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"owner"`
	Tracks *struct {
		Total int `json:"total"`
	} `json:"tracks"`
	Images []Image `json:"images"`
}

func (p playlistJSON) playlist() Playlist {
	playlist := Playlist{
		ID:          p.ID,
		URI:         p.URI,
		Name:        p.Name,
		Description: p.Description,
		Images:      p.Images,
	}
	if p.Owner != nil {
		playlist.Owner = p.Owner.DisplayName
		if playlist.Owner == "" {
			playlist.Owner = p.Owner.ID
		}
	}
	if p.Tracks != nil {
		playlist.TotalTracks = p.Tracks.Total
	}
	return playlist
}
