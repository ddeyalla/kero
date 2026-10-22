package web

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	// TrackImageWidth is the width in device pixels that a track row asks
	// of its cover. A row shows a small cover, so the 64 px one is enough.
	TrackImageWidth = 64

	// maxTracks caps the tracks one list loads, to bound the memory of a
	// huge playlist.
	maxTracks = 500
	// trackPageSize is the most tracks the API gives in one page.
	trackPageSize = 50
	// trackListCacheSize is how many track lists the client keeps.
	trackListCacheSize = 8
)

// ListTrack is one playable track of a playlist, an album or the queue.
type ListTrack struct {
	ID         string
	URI        string
	Name       string
	Artists    []string
	Album      string
	DurationMS int
	// Artwork is the URL of a small cover, "" when there is none. The
	// tracks of an album have no cover of their own, so AlbumTracks gives
	// them the cover that the caller passes.
	Artwork string
}

// ArtistLine joins the artists for the second line of a row.
func (t ListTrack) ArtistLine() string { return strings.Join(t.Artists, ", ") }

// listTrackJSON is a track as the API writes it. IsPlayable is a pointer,
// because the API leaves it out unless it relinks tracks for a market.
type listTrackJSON struct {
	ID         string `json:"id"`
	URI        string `json:"uri"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	IsLocal    bool   `json:"is_local"`
	IsPlayable *bool  `json:"is_playable"`
	DurationMS int    `json:"duration_ms"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album *struct {
		Name   string  `json:"name"`
		Images []Image `json:"images"`
	} `json:"album"`
}

// track converts the JSON to a ListTrack. It returns false for what the
// player cannot play: a missing item, an episode, a local file, a track
// that is unavailable in the user's market, and one with no URI.
func (t *listTrackJSON) track(artwork string) (ListTrack, bool) {
	if t == nil || t.URI == "" || t.ID == "" || t.IsLocal {
		return ListTrack{}, false
	}
	if t.Type != "" && t.Type != "track" {
		return ListTrack{}, false
	}
	if t.IsPlayable != nil && !*t.IsPlayable {
		return ListTrack{}, false
	}
	track := ListTrack{ID: t.ID, URI: t.URI, Name: t.Name, DurationMS: t.DurationMS, Artwork: artwork}
	for _, artist := range t.Artists {
		if artist.Name != "" {
			track.Artists = append(track.Artists, artist.Name)
		}
	}
	if t.Album != nil {
		track.Album = t.Album.Name
		if artwork == "" {
			track.Artwork = PickImage(t.Album.Images, TrackImageWidth)
		}
	}
	return track, true
}

// trackFields asks the API for only what ListTrack needs.
const trackFields = "id,uri,name,type,is_local,is_playable,duration_ms,artists(name),album(name,images)"

// trackList is the tracks of one playlist or album.
type trackList struct {
	key    string
	tracks []ListTrack
}

// rememberTracks keeps a track list as the newest of the last
// trackListCacheSize, unless a sign-in or sign-out came since gen.
func (c *Client) rememberTracks(gen uint64, key string, tracks []ListTrack) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.gen != gen {
		return
	}
	kept := c.trackLists[:0]
	for _, entry := range c.trackLists {
		if entry.key != key {
			kept = append(kept, entry)
		}
	}
	kept = append(kept, trackList{key: key, tracks: tracks})
	if len(kept) > trackListCacheSize {
		// Clear the dropped entries, so that they can be freed.
		clear(kept[:len(kept)-trackListCacheSize])
		kept = kept[len(kept)-trackListCacheSize:]
	}
	c.trackLists = kept
}

func (c *Client) cachedTracks(key string) ([]ListTrack, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	for _, entry := range c.trackLists {
		if entry.key == key {
			return slices.Clone(entry.tracks), true
		}
	}
	return nil, false
}

// CachedPlaylistTracks is the tracks of a playlist from an earlier
// PlaylistTracks call, and whether there was one. It sends no request.
func (c *Client) CachedPlaylistTracks(playlistID string) ([]ListTrack, bool) {
	return c.cachedTracks("playlist/" + playlistID)
}

// CachedAlbumTracks is the tracks of an album from an earlier AlbumTracks
// call, and whether there was one. It sends no request.
func (c *Client) CachedAlbumTracks(albumID string) ([]ListTrack, bool) {
	return c.cachedTracks("album/" + albumID)
}

// loadTracks runs a track list load shared by the callers that ask for the
// same list at the same time, keeps its answer, and on a network or rate
// limit failure returns the list of an earlier call with the error.
func (c *Client) loadTracks(ctx context.Context, key string, load func(context.Context) ([]ListTrack, error)) ([]ListTrack, error) {
	val, err := c.shared(ctx, "tracks/"+key, func(ctx context.Context) (any, error) {
		gen := c.cacheGen()
		tracks, err := load(ctx)
		if err != nil {
			return nil, err
		}
		c.rememberTracks(gen, key, tracks)
		return tracks, nil
	})
	if err != nil {
		if keepsResults(err) {
			if last, ok := c.cachedTracks(key); ok {
				return last, err
			}
		}
		return nil, err
	}
	tracks, _ := val.([]ListTrack)
	return slices.Clone(tracks), nil
}

// PlaylistTracks returns the playable tracks of a playlist, up to the
// first 500 items. It skips local files, episodes, empty items and tracks
// that are unavailable in the user's market. Calls for the same playlist
// at the same time share one request.
//
// It needs the playlist-read-private scope, and ScopeCollaborative for a
// collaborative playlist. Spotify answers 403 for a playlist that the user
// neither owns nor collaborates on.
func (c *Client) PlaylistTracks(ctx context.Context, playlistID string) ([]ListTrack, error) {
	id := strings.TrimSpace(playlistID)
	if id == "" {
		return nil, nil
	}
	return c.loadTracks(ctx, "playlist/"+id, func(ctx context.Context) ([]ListTrack, error) {
		tracks, err := c.pagePlaylist(ctx, "/v1/playlists/"+url.PathEscape(id)+"/items")
		if err != nil && strings.Contains(err.Error(), "(HTTP 404)") {
			// The older endpoint of Spotify, for an API that does not
			// have /items. A playlist that is gone fails the same way.
			if old, oldErr := c.pagePlaylist(ctx, "/v1/playlists/"+url.PathEscape(id)+"/tracks"); oldErr == nil {
				return old, nil
			}
		}
		return tracks, err
	})
}

// pagePlaylist reads the pages of one playlist endpoint. A page is read
// as either {"item": ...} (current) or {"track": ...} (deprecated).
func (c *Client) pagePlaylist(ctx context.Context, path string) ([]ListTrack, error) {
	var tracks []ListTrack
	for offset := 0; len(tracks) < maxTracks; {
		params := url.Values{}
		params.Set("limit", strconv.Itoa(trackPageSize))
		params.Set("offset", strconv.Itoa(offset))
		params.Set("market", "from_token")
		params.Set("fields", "next,items(is_local,item("+trackFields+"),track("+trackFields+"))")
		var body struct {
			Next  string `json:"next"`
			Items []*struct {
				Item  *listTrackJSON `json:"item"`
				Track *listTrackJSON `json:"track"`
			} `json:"items"`
		}
		if err := c.get(ctx, path, params, &body); err != nil {
			return nil, err
		}
		for _, entry := range body.Items {
			if entry == nil {
				continue
			}
			item := entry.Item
			if item == nil {
				item = entry.Track
			}
			if track, ok := item.track(""); ok && len(tracks) < maxTracks {
				tracks = append(tracks, track)
			}
		}
		offset += len(body.Items)
		if body.Next == "" || len(body.Items) == 0 {
			break
		}
	}
	return tracks, nil
}

// AlbumTracks returns the playable tracks of an album, up to the first
// 500. The API gives album tracks no cover, so every track gets the
// artwork that the caller passes, which is Album.Artwork of the album;
// it may be "". Their Album field is empty, because the API leaves the
// album out of its own tracks. Calls for the same album at the same time share one
// request.
//
// It needs no scope beyond a valid token.
func (c *Client) AlbumTracks(ctx context.Context, albumID, artwork string) ([]ListTrack, error) {
	id := strings.TrimSpace(albumID)
	if id == "" {
		return nil, nil
	}
	return c.loadTracks(ctx, "album/"+id, func(ctx context.Context) ([]ListTrack, error) {
		var tracks []ListTrack
		for offset := 0; len(tracks) < maxTracks; {
			params := url.Values{}
			params.Set("limit", strconv.Itoa(trackPageSize))
			params.Set("offset", strconv.Itoa(offset))
			params.Set("market", "from_token")
			var body struct {
				Next  string           `json:"next"`
				Items []*listTrackJSON `json:"items"`
			}
			if err := c.get(ctx, "/v1/albums/"+url.PathEscape(id)+"/tracks", params, &body); err != nil {
				return nil, err
			}
			for _, item := range body.Items {
				if track, ok := item.track(artwork); ok && len(tracks) < maxTracks {
					tracks = append(tracks, track)
				}
			}
			offset += len(body.Items)
			if body.Next == "" || len(body.Items) == 0 {
				break
			}
		}
		return tracks, nil
	})
}

// Queue returns the track that plays now and the tracks that come next
// (the user's Up next). current is nil when nothing plays or an episode
// does; episodes in the queue are skipped. The queue is not cached: it
// changes with every skip.
//
// It needs user-read-playback-state, which DefaultScopes holds, or
// user-read-currently-playing.
func (c *Client) Queue(ctx context.Context) (current *ListTrack, queue []ListTrack, err error) {
	var body struct {
		CurrentlyPlaying *listTrackJSON   `json:"currently_playing"`
		Queue            []*listTrackJSON `json:"queue"`
	}
	if err := c.get(ctx, "/v1/me/player/queue", nil, &body); err != nil {
		return nil, nil, err
	}
	if track, ok := body.CurrentlyPlaying.track(""); ok {
		current = &track
	}
	for _, item := range body.Queue {
		if track, ok := item.track(""); ok {
			queue = append(queue, track)
		}
	}
	if len(queue) > maxTracks {
		queue = queue[:maxTracks]
	}
	return current, queue, nil
}
