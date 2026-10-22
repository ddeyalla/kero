package web

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

// ErrMissingScope says the client's tokens lack a scope that a call needs.
// It is not a refused token: the user may go on without that call.
var ErrMissingScope = errors.New("spotify: the sign-in lacks a scope")

// Track is a track the pane shows before the player holds one: what plays
// on another device, or what the user played last.
type Track struct {
	URI      string
	Name     string
	Artists  []string
	Album    string
	Image    string
	Duration time.Duration
	// Position is how far the track had played. It is zero for a track
	// from the history.
	Position time.Duration
	// ContextURI is the album or the playlist the track played in, or ""
	// when it played alone or in a context that cannot start at a track.
	ContextURI string
	// OtherDevice says the track plays, or is held, on another device of
	// the user, so that playing it here is a transfer.
	OtherDevice bool
}

// ArtistLine joins the artists for the second line of the pane.
func (t Track) ArtistLine() string { return strings.Join(t.Artists, ", ") }

// trackJSON is a track as the player endpoints write it. Episodes have no
// album and are not tracks, so they carry another type.
type trackJSON struct {
	URI        string `json:"uri"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	DurationMS int64  `json:"duration_ms"`
	Artists    []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album *struct {
		Name   string  `json:"name"`
		Images []Image `json:"images"`
	} `json:"album"`
}

// contextJSON is the album or the playlist that a track plays in.
type contextJSON struct {
	URI  string `json:"uri"`
	Type string `json:"type"`
}

// uri is the URI of a context that a play can start at a track, or "".
func (c *contextJSON) uri() string {
	if c == nil || (c.Type != "album" && c.Type != "playlist") {
		return ""
	}
	return c.URI
}

func (t trackJSON) track(in *contextJSON) *Track {
	if t.URI == "" || t.Name == "" || (t.Type != "" && t.Type != "track") {
		return nil
	}
	track := &Track{
		URI:        t.URI,
		Name:       t.Name,
		Duration:   time.Duration(t.DurationMS) * time.Millisecond,
		ContextURI: in.uri(),
	}
	for _, artist := range t.Artists {
		if artist.Name != "" {
			track.Artists = append(track.Artists, artist.Name)
		}
	}
	if t.Album != nil {
		track.Album = t.Album.Name
		track.Image = PickImage(t.Album.Images, TileImageWidth)
	}
	return track
}

// CurrentTrack returns the track that plays, or is held, on a device of
// the user, or nil when no device holds one. The call needs the playback
// state scope, which every sign-in has.
func (c *Client) CurrentTrack(ctx context.Context) (*Track, error) {
	var body struct {
		ProgressMS int64        `json:"progress_ms"`
		Context    *contextJSON `json:"context"`
		Item       *trackJSON   `json:"item"`
	}
	params := url.Values{}
	params.Set("additional_types", "track")
	if err := c.get(ctx, "/v1/me/player", params, &body); err != nil {
		return nil, err
	}
	if body.Item == nil {
		return nil, nil
	}
	track := body.Item.track(body.Context)
	if track == nil {
		return nil, nil
	}
	track.Position = time.Duration(body.ProgressMS) * time.Millisecond
	track.OtherDevice = true
	return track, nil
}

// LastPlayed returns the track the user played last, or nil when the
// history is empty. Tokens without the recently-played scope give
// ErrMissingScope and send nothing, so that the call cannot be the reason
// for a refused token.
func (c *Client) LastPlayed(ctx context.Context) (*Track, error) {
	if !c.HasScope(ScopeRecentlyPlayed) {
		return nil, ErrMissingScope
	}
	var body struct {
		Items []struct {
			Track   *trackJSON   `json:"track"`
			Context *contextJSON `json:"context"`
		} `json:"items"`
	}
	params := url.Values{}
	params.Set("limit", "1")
	if err := c.get(ctx, "/v1/me/player/recently-played", params, &body); err != nil {
		return nil, err
	}
	if len(body.Items) == 0 || body.Items[0].Track == nil {
		return nil, nil
	}
	return body.Items[0].Track.track(body.Items[0].Context), nil
}
