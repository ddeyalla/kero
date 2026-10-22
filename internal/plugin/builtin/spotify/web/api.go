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
	"time"
	"unicode/utf8"
)

// maxResponse caps the body of one Web API answer.
const maxResponse = 8 << 20

const (
	// searchCacheSize is how many queries CachedSearch remembers, and
	// searchCacheTTL how long it believes an answer.
	searchCacheSize = 20
	searchCacheTTL  = 2 * time.Minute
)

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

// Artwork is the cover URL for a grid tile, or "" when Spotify gave no
// cover. See PickImage for which cover that is.
func (a Album) Artwork() string { return PickImage(a.Images, TileImageWidth) }

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

// Artwork is the cover URL for a grid tile, or "" when Spotify gave none.
func (p Playlist) Artwork() string { return PickImage(p.Images, TileImageWidth) }

// SearchResult is what a search found.
type SearchResult struct {
	// Query is the trimmed query the results are for. The last results
	// a failed search returns can be for another query.
	Query     string
	Albums    []Album
	Playlists []Playlist
}

// TileImageWidth is the width in device pixels that a grid tile asks of
// its cover. A tile is 160 to 200 points wide and the display may be 2x, so
// the 64 px cover looks blurred, and the 640 px one costs memory and time
// for nothing. Spotify gives covers of 640, 300 and 64 px, so this picks
// the 300 px one, and the same URL serves the tile, the drill-in header and
// the row that plays.
const TileImageWidth = 300

// PickImage returns the URL of the smallest image that is at least minWidth
// pixels wide, so that a cover is sharp but not larger than it needs to
// be. When no image is that wide it returns the widest one. Images that
// give no width count as the narrowest; when none gives a width, the first
// image is used, which Spotify lists as the largest. It returns "" when
// there are no images.
func PickImage(images []Image, minWidth int) string {
	if len(images) == 0 {
		return ""
	}
	pick := -1
	for i, img := range images {
		if img.Width < minWidth {
			continue
		}
		if pick < 0 || img.Width < images[pick].Width {
			pick = i
		}
	}
	if pick >= 0 {
		return images[pick].URL
	}
	pick = 0
	for i, img := range images[1:] {
		if img.Width > images[pick].Width {
			pick = i + 1
		}
	}
	return images[pick].URL
}

// Search finds albums and playlists. An empty query finds nothing and
// sends no request.
//
// When the network is down or Spotify asks to wait, Search returns the
// last results together with ErrNetworkDown or a *RateLimitError, so the
// pane can keep them and add a note. Their Query tells what they are for.
func (c *Client) Search(ctx context.Context, query string) (SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return SearchResult{}, nil
	}
	c.cacheMu.Lock()
	gen := c.gen
	c.searchSeq++
	seq := c.searchSeq
	c.cacheMu.Unlock()
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
		if keepsResults(err) {
			if last, ok := c.lastSearchResults(); ok {
				return last, err
			}
		}
		return SearchResult{}, err
	}
	result := SearchResult{Query: q}
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
	// Keep the result only while no sign-in or sign-out came between
	// and no newer search got its answer first.
	c.cacheMu.Lock()
	if c.gen == gen {
		c.rememberSearchLocked(result)
	}
	if c.gen == gen && seq > c.lastSearchSeq {
		c.lastSearch = result
		c.lastSearchSet = true
		c.lastSearchSeq = seq
	}
	c.cacheMu.Unlock()
	return result, nil
}

// searchKey is what two spellings of one query share: Spotify does not
// tell upper and lower case apart.
func searchKey(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// cachedSearch is the answer to one query and the time it came.
type cachedSearch struct {
	key    string
	result SearchResult
	at     time.Time
}

// rememberSearchLocked keeps the answer to a query as the newest of the
// last searchCacheSize, and is called with cacheMu held.
func (c *Client) rememberSearchLocked(result SearchResult) {
	key := searchKey(result.Query)
	kept := c.searches[:0]
	for _, entry := range c.searches {
		if entry.key != key {
			kept = append(kept, entry)
		}
	}
	kept = append(kept, cachedSearch{key: key, result: result, at: c.now()})
	if len(kept) > searchCacheSize {
		kept = kept[len(kept)-searchCacheSize:]
	}
	c.searches = kept
}

// CachedSearch returns the answer of an earlier search of this query, if
// it is less than two minutes old, and whether there was one. It sends no
// request: the pane shows it at once, and still asks Search for the news.
// The result's Query is the query as it is spelled here.
func (c *Client) CachedSearch(query string) (SearchResult, bool) {
	q := strings.TrimSpace(query)
	key := searchKey(q)
	if key == "" {
		return SearchResult{}, false
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	for _, entry := range c.searches {
		if entry.key == key && c.now().Sub(entry.at) < searchCacheTTL {
			result := entry.result
			result.Query = q
			return result, true
		}
	}
	return SearchResult{}, false
}

// flight is one library load in flight, and what it answered.
type flight struct {
	done chan struct{}
	val  any
	err  error
}

// libraryLoadTimeout bounds one shared library load. The load does not take
// the context of the caller that started it, because the others wait for
// its answer too, so it has a deadline of its own.
const libraryLoadTimeout = 8 * time.Second

// shared runs load once for all the callers that ask for the same name at
// the same time: the others wait for its answer. The load runs under its
// own deadline, not the context of the caller that started it, so that
// this caller giving up does not fail the others. A caller whose context
// ends first stops waiting, and the load goes on for the rest. The flight
// is keyed by the account generation too, so a call after a sign-out and a
// sign-in does not join the load of the old account.
func (c *Client) shared(ctx context.Context, name string, load func(context.Context) (any, error)) (any, error) {
	key := fmt.Sprintf("%d/%s", c.cacheGen(), name)
	c.flightMu.Lock()
	if c.flights == nil {
		c.flights = make(map[string]*flight)
	}
	f, ok := c.flights[key]
	if !ok {
		f = &flight{done: make(chan struct{})}
		c.flights[key] = f
		go func() {
			loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), libraryLoadTimeout)
			defer cancel()
			f.val, f.err = load(loadCtx)
			c.flightMu.Lock()
			delete(c.flights, key)
			c.flightMu.Unlock()
			close(f.done)
		}()
	}
	c.flightMu.Unlock()
	select {
	case <-f.done:
		return f.val, f.err
	case <-ctx.Done():
		return nil, networkError(ctx, ctx.Err())
	}
}

// cacheGen is the generation a call starts with.
func (c *Client) cacheGen() uint64 {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return c.gen
}

// Playlists returns the user's playlists, up to the first 50. Calls at
// the same time share one request.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	val, err := c.shared(ctx, "playlists", func(ctx context.Context) (any, error) { return c.loadPlaylists(ctx) })
	list, _ := val.([]Playlist)
	return list, err
}

func (c *Client) loadPlaylists(ctx context.Context) ([]Playlist, error) {
	gen := c.cacheGen()
	params := url.Values{}
	params.Set("limit", "50")
	var body struct {
		Items []*playlistJSON `json:"items"`
	}
	if err := c.get(ctx, "/v1/me/playlists", params, &body); err != nil {
		if keepsResults(err) {
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
	if c.gen == gen {
		c.lastPlaylists = list
		c.lastPlaylistsSet = true
	}
	c.cacheMu.Unlock()
	return list, nil
}

// Albums returns the user's saved albums, up to the first 50. Calls at
// the same time share one request.
func (c *Client) Albums(ctx context.Context) ([]Album, error) {
	val, err := c.shared(ctx, "albums", func(ctx context.Context) (any, error) { return c.loadAlbums(ctx) })
	list, _ := val.([]Album)
	return list, err
}

func (c *Client) loadAlbums(ctx context.Context) ([]Album, error) {
	gen := c.cacheGen()
	params := url.Values{}
	params.Set("limit", "50")
	var body struct {
		Items []*struct {
			Album *albumJSON `json:"album"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/v1/me/albums", params, &body); err != nil {
		if keepsResults(err) {
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
	if c.gen == gen {
		c.lastAlbums = list
		c.lastAlbumsSet = true
	}
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
// once: one stale token must not look like a revoked sign-in. While a
// rate limit lasts, get sends nothing, not even a refresh; the states
// without a call, such as signed out, still come first.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return err
	}
	if err := c.limited(); err != nil {
		return err
	}
	err = c.doGet(ctx, path, params, token, out)
	if errors.Is(err, errTokenStale) {
		token, err = c.refreshAfterRefused(ctx, token)
		if err != nil {
			return err
		}
		err = c.doGet(ctx, path, params, token, out)
	}
	if errors.Is(err, errTokenStale) {
		// The API refused the token the refresh just gave: refreshing
		// again on every call would not help, so the user signs in again.
		c.tokenMu.Lock()
		current := c.tok
		c.tokenMu.Unlock()
		if current != nil && current.AccessToken == token {
			c.refuseIf(current)
		}
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
		return networkError(ctx, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return networkError(ctx, err)
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
		// The token is valid, but the app or the user may not use this
		// endpoint. That is not the same as a refused token.
		return fmt.Errorf("%w: %s", ErrForbidden, message)
	case resp.StatusCode == http.StatusTooManyRequests:
		return c.rateLimited(resp, spotifyMessage(raw))
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: HTTP %d: %s", ErrNetworkDown, resp.StatusCode, spotifyMessage(raw))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("spotify: request failed (HTTP %d): %s", resp.StatusCode, spotifyMessage(raw))
	}
	// An answer with no content, such as the player of a user with no
	// device, leaves out as it is.
	if resp.StatusCode == http.StatusNoContent || len(raw) == 0 {
		return nil
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
