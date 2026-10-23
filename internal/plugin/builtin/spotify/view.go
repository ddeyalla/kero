package spotify

import (
	"fmt"
	"strconv"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/locales"
)

// The pane's messages. They are kept as values so that the tests and the
// code below name them once.
var (
	localeCommandFailed  = locales.SpotifyCommandFailed
	localeSeekFailed     = locales.SpotifySeekFailed
	localeVolumeFailed   = locales.SpotifyVolumeFailed
	localePlayFailed     = locales.SpotifyPlayFailed
	localeSignInRunning  = locales.SpotifySignInRunning
	localeSignInFailed   = locales.SpotifySignInFailed
	localeNoClientID     = locales.SpotifyNoClientID
	localeAlbum          = locales.SpotifyAlbum
	localePlaylist       = locales.SpotifyPlaylist
	localePlaybackFailed = locales.SpotifyPlaybackFailed
)

// layout is how much of the player a pane has room for.
type layout int

const (
	// layoutSmall is the player alone: the art, the track, the play
	// button and a thin progress bar.
	layoutSmall layout = iota
	// layoutMedium adds the previous and next buttons and one row of
	// album and playlist tiles.
	layoutMedium
	// layoutLarge is the whole player: two compact rows of art, track,
	// controls, seek and volume, and the library as a grid of tiles.
	layoutLarge
)

// The sizes the layouts change at, and the room around the pane's parts.
const (
	smallBelowWidth   = 300
	smallBelowHeight  = 230
	mediumBelowHeight = 420
	panePadding       = 16
	// gridGap is the host's space between grid tiles.
	gridGap = 12
	// tileCaption is the room a tile's title and subtitle take under its
	// picture.
	tileCaption = 40
	// wideFrom is the pane width from which the controls sit beside the
	// track in one row and the volume beside the seek bar; a narrower pane
	// gives the controls, the seek bar and the volume a row each.
	wideFrom = 440
	// tileTarget is the width a grid tile aims at.
	tileTarget = 180
	// volumeWidth is the width of the volume slider.
	volumeWidth = 80
)

// layoutOf is the layout a pane of this size draws. A pane whose size is
// not known yet draws the large layout.
func layoutOf(size protocol.Size) layout {
	switch {
	case size.Width > 0 && size.Width < smallBelowWidth, size.Height > 0 && size.Height < smallBelowHeight:
		return layoutSmall
	case size.Height > 0 && size.Height < mediumBelowHeight:
		return layoutMedium
	default:
		return layoutLarge
	}
}

// tree builds the pane: the sign-in that playing here needs, or the
// player in the layout the pane has room for. The medium and the large
// layouts scroll as one, the head with the library, where the small one
// fits the pane. A pane with no track loaded draws the library alone, and a
// pane that is still opening draws nothing yet, so that it never shows a
// sign-in it has not checked.
func (p *plugin) tree(s *session) protocol.Tree {
	if s.opening {
		return loadingTree()
	}
	if p.engine == nil {
		return p.gateTree(s).Tree()
	}
	if !s.showsPlayer() && !s.showsPlayerState() {
		return p.libraryOnlyTree(s)
	}
	switch layoutOf(s.size) {
	case layoutSmall:
		return p.smallTree(s).Tree()
	case layoutMedium:
		return p.mediumTree(s).ScrollingTree()
	default:
		return p.largeTree(s).ScrollingTree()
	}
}

// loadingTree is the pane while it opens, before it knows whether the user
// is signed in: an empty pane, which the first real tree replaces.
func loadingTree() protocol.Tree {
	return columnNodes(widgetPlayer).Padding(panePadding).Tree()
}

// showsPlayer tells whether the pane draws the player: a track is loaded in
// this session, playing or paused. The track played last, which an idle
// player only shows, is no track loaded.
func (s *session) showsPlayer() bool {
	return s.playback == modePlaying && s.last == nil
}

// showsPlayerState tells whether the pane draws the empty state of a player
// that failed, such as the desktop app that is not there. A player with
// nothing to play, or that is still starting, draws no state at all.
func (s *session) showsPlayerState() bool {
	switch s.playback {
	case modeUnavailable, modeNotRunning, modeNotInstalled, modeAutomation:
		return true
	}
	return false
}

// libraryOnlyTree is the pane of a player with nothing loaded: the library
// alone, in the whole pane, in any layout.
func (p *plugin) libraryOnlyTree(s *session) protocol.Tree {
	padding := panePadding
	if layoutOf(s.size) == layoutSmall {
		padding = 12
	}
	return columnNodes(widgetPlayer, s.noticeNode(), p.libraryTree(s)).Gap(12).Padding(padding).ScrollingTree()
}

// gateTree is the pane before it can play: music plays in the app once
// the user signs in with the streaming scope, so the pane asks for that
// sign-in and nothing else. Its node ids are the actions it offers.
func (p *plugin) gateTree(s *session) *sdk.Node {
	var state *sdk.Node
	switch {
	case s.signingIn:
		state = sdk.EmptyState("state-signing-in", locales.SpotifySignInRunning(), "")
	case s.lib == libNoClientID:
		state = sdk.EmptyState("setup", locales.SpotifyNoClientID(), locales.SpotifyNoClientIDDetail(p.redirectURI())).
			Action(locales.SpotifySetUp())
	case s.lib == libSignedOut:
		state = sdk.EmptyState("sign-in", locales.SpotifyPlayHereTitle(), locales.SpotifyPlayHereDetail()).
			Action(locales.SpotifySignIn())
	case s.lib == libPremiumLapsed:
		state = sdk.EmptyState("state-premium", locales.SpotifyPremium(), locales.SpotifyPremiumDetail())
	case s.lib == libReady && p.streamable:
		// The sign-in is right but the engine did not start: another
		// sign-in would not help.
		state = sdk.EmptyState("state-unavailable", locales.SpotifyUnavailable(), locales.SpotifyUnavailableDetail())
	default:
		// Signed in without the streaming scope, or with a token Spotify
		// refused: a new sign-in fixes both.
		state = sdk.EmptyState("sign-in-again", locales.SpotifyStreamAgainTitle(), locales.SpotifyStreamAgainDetail()).
			Action(locales.SpotifySignInAgain())
	}
	return columnNodes(widgetPlayer, state.Grow(1), s.noticeNode()).Gap(8).Padding(panePadding)
}

// smallTree is the smallest player, like Spotify's small widget: the art,
// the title and the artist, a play button, and a thin progress bar.
func (p *plugin) smallTree(s *session) *sdk.Node {
	if s.playback != modePlaying {
		return columnNodes(widgetPlayer, p.playbackState(s).Grow(1), s.noticeNode()).Gap(4).Padding(12)
	}
	head := rowNodes("head", s.artNode("art", 48, 6), s.metaNode(false), sdk.IconButton("play", s.playIcon()).Primary().Size(40, 40)).Gap(10)
	return columnNodes(widgetPlayer, head, sdk.Spacer("fill"), s.progressNode(), s.noticeNode()).Gap(10).Padding(12)
}

// mediumTree is the player with one row of tiles under it, like Spotify's
// medium widget.
func (p *plugin) mediumTree(s *session) *sdk.Node {
	var head *sdk.Node
	if s.playback != modePlaying {
		head = p.playbackState(s)
	} else {
		head = rowNodes("head",
			s.artNode("art", 64, 6),
			s.metaNode(false),
			sdk.IconButton("prev", "previous").Size(32, 32),
			sdk.IconButton("play", s.playIcon()).Primary().Size(44, 44),
			sdk.IconButton("next", "next").Size(32, 32),
		).Gap(10)
	}
	children := []*sdk.Node{head}
	if s.playback == modePlaying {
		children = append(children, s.progressNode())
	}
	children = append(children, s.noticeNode())

	// One row of tiles, as many as fit, no taller than the room left.
	used := panePadding*2 + 64 + 14
	if s.playback == modePlaying {
		used += 4 + 14
	}
	if tile, cols := tileFit(s.size.Width, 96); cols > 0 {
		if room := s.size.Height - used - tileCaption; s.size.Height > 0 && room < tile {
			// Shorter tiles: more of them fit the row.
			tile = room
			cols = max(1, (rowRoom(s.size.Width)+gridGap)/(tile+gridGap))
		}
		if items := s.items(cols); tile >= 48 && len(items) > 0 {
			children = append(children, sdk.List("results", items...).Grid(tile).Grow(1))
		}
	}
	return columnNodes(widgetPlayer, children...).Gap(14).Padding(panePadding)
}

// largeTree is the whole player in two compact rows, like Spotify's own
// bar: the art, the track and the controls, then the seek bar and the
// volume, with the library under them as a grid of tiles. A narrow pane
// gives the controls and the volume a row each.
func (p *plugin) largeTree(s *session) *sdk.Node {
	children := make([]*sdk.Node, 0, 8)
	if s.playback != modePlaying {
		// The state of a player that failed; the library stays usable.
		children = append(children, p.playbackState(s))
	} else {
		children = append(children, s.playerRows(s.size.Width == 0 || s.size.Width >= wideFrom)...)
	}
	children = append(children, s.noticeNode(), p.libraryTree(s))
	return columnNodes(widgetPlayer, children...).Gap(12).Padding(panePadding)
}

// playerRows are the rows of the large player. The wide ones are two: the
// art, the title and the artist, then shuffle, previous, play, next and
// repeat; and under them the elapsed time, the seek bar, the duration, the
// speaker and a short volume slider. The narrow ones are three, with the
// controls and the volume on a row each.
func (s *session) playerRows(wide bool) []*sdk.Node {
	const gap = 10
	shuffle := sdk.IconButton("shuffle", "shuffle").Label(locales.SpotifyShuffle()).Size(28, 28).Active(s.snap.Shuffle)
	prev := sdk.IconButton("prev", "previous").Size(32, 32)
	play := sdk.IconButton("play", s.playIcon()).Primary().Size(40, 40)
	next := sdk.IconButton("next", "next").Size(32, 32)
	repeat := sdk.IconButton("repeat", "repeat").Label(locales.SpotifyRepeat()).Size(28, 28).Active(s.snap.Repeat)

	seek, elapsed, duration := s.seekNodes()
	// A muted player shows the crossed speaker, and the toggle in the
	// accent.
	speaker := sdk.IconButton("mute", "volume").Label(locales.SpotifyVolume())
	if s.snap.Volume == 0 {
		speaker = sdk.IconButton("mute", "volume-off").Label(locales.SpotifyVolume()).Active(true)
	}
	volume := sdk.Slider("volume", float64(s.snap.Volume), 0, 100).Step(1).Size(volumeWidth, 0)

	if wide {
		return []*sdk.Node{
			rowNodes("head", s.artNode("art", 56, 6), s.metaNode(false), shuffle, prev, play, next, repeat).Gap(gap),
			rowNodes("seek-row", elapsed, seek.Grow(1), duration, speaker, volume).Gap(gap),
		}
	}
	return []*sdk.Node{
		rowNodes("head", s.artNode("art", 56, 6), s.metaNode(false)).Gap(gap),
		sdk.Row("controls", shuffle, sdk.Spacer("controls-left"), prev, play, next, sdk.Spacer("controls-right"), repeat).Gap(gap),
		rowNodes("seek-row", elapsed, seek.Grow(1), duration).Gap(gap),
		sdk.Row("volume-row", speaker, volume.Size(0, 0).Grow(1)).Gap(gap),
	}
}

// seekNodes are the seek slider and the two times. The tree carries the
// position the snapshot was read at, and the instant of that read, and the
// host runs the slider and the elapsed clock on from there by itself: the
// tree changes when the playback does, never because time passed. A paused
// or stopped track has no rate and no instant.
func (s *session) seekNodes() (seek, elapsed, duration *sdk.Node) {
	total := s.snap.Duration.Seconds()
	pos := min(max(s.snap.Position.Seconds(), 0), total)
	rate, at := 0.0, int64(0)
	if s.snap.State == transport.StatePlaying && !s.snap.ReadAt.IsZero() && total > 0 {
		rate, at = 1, s.snap.ReadAt.UnixMilli()
	}
	seek = sdk.Slider("seek", pos, 0, max(total, 1)).Step(1).Playing(rate, at)
	if total <= 0 {
		seek.Disabled()
	}
	elapsed = sdk.Clock("elapsed", pos, total).Role(protocol.RoleCaption).Playing(rate, at)
	duration = sdk.Text("duration", clock(s.snap.Duration)).Role(protocol.RoleCaption)
	return seek, elapsed, duration
}

// rowRoom is the width a row of tiles has in a pane this wide: the pane
// without its padding, and a little for the list's scroll bar. A pane whose
// size is not known yet counts as 480 wide.
func rowRoom(width int) int {
	if width <= 0 {
		width = 480
	}
	return width - panePadding*2 - 4
}

// metaNode is the track's title and artist, and in the large layout its
// album too.
func (s *session) metaNode(large bool) *sdk.Node {
	title := sdk.Text("title", s.snap.Track).Role(protocol.RoleTitle).Lines(1).Marquee()
	if large {
		title.Role(protocol.RoleHeadline)
	}
	meta := []*sdk.Node{title, sdk.Text("artist", s.snap.Artist).Role(protocol.RoleCaption).Lines(1)}
	if large && s.snap.Album != "" {
		meta = append(meta, sdk.Text("album", s.snap.Album).Role(protocol.RoleCaption).Lines(1))
	}
	return sdk.Column("meta", meta...).Gap(2).Grow(1)
}

// tileFit is the width of a grid tile and the number of tiles in a row,
// for a pane this wide and tiles at least min wide. The host puts gridGap
// between tiles; a pixel is left over so rounding never wraps the last
// tile.
func tileFit(width, min int) (tile, cols int) {
	room := rowRoom(width)
	cols = max(2, (room+gridGap)/(min+gridGap))
	tile = (room-gridGap*(cols-1))/cols - 1
	if tile < 1 {
		return 0, 0
	}
	return tile, cols
}

// gridFit is the width of a tile in the library grid and the number of
// tiles in a row: the count, from two up, whose tile is nearest tileTarget
// wide. A pane too narrow for that gets two tiles, however small.
func gridFit(width int) (tile, cols int) {
	room := rowRoom(width)
	best := -1
	for c := 2; c <= 12; c++ {
		w := (room-gridGap*(c-1))/c - 1
		if w < 1 {
			break
		}
		if best < 0 || abs(w-tileTarget) < abs(best-tileTarget) {
			best, cols = w, c
		}
	}
	if best < 1 {
		return 0, 0
	}
	return best, cols
}

// abs is the size of a difference.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// progressNode is the compact player's thin bar. While a track plays, the
// host moves the bar on its own by rate from at, so the bar carries the
// snapshot's fraction at its read time, not the position now: the time
// since the read would count twice. The large layout's seek slider is
// moved by the host in the same way, and carries the snapshot's position
// at its read time.
func (s *session) progressNode() *sdk.Node {
	if s.snap.State != transport.StatePlaying || s.snap.ReadAt.IsZero() {
		return sdk.Progress("progress", s.fraction())
	}
	return sdk.Progress("progress", s.snap.Fraction()).Playing(s.snap.Rate(), s.snap.At())
}

// noticeNode is the line that says a command failed, or nil when no
// command failed since the last one that worked. Every layout draws it.
func (s *session) noticeNode() *sdk.Node {
	if s.notice == "" {
		return nil
	}
	return sdk.Text("notice", s.notice).Role(protocol.RoleCaption).Color(protocol.ColorAccent).Lines(2)
}

// playbackState is the empty state of a player that cannot play. The
// desktop app's states are the desktop player's, which the engine replaces:
// the pane never asks to open Spotify.
func (p *plugin) playbackState(s *session) *sdk.Node {
	return sdk.EmptyState("state-unavailable", locales.SpotifyUnavailable(), locales.SpotifyUnavailableDetail())
}

// libraryTree is the tabs and the library content, or the tracks of a
// playlist or an album in place of the tabs. A library state that is not
// ready replaces the content, but the tabs stay so that a press still
// explains the sign-in that is missing.
func (p *plugin) libraryTree(s *session) *sdk.Node {
	if s.drill != "" && s.lib == libReady {
		return p.drillTree(s)
	}
	tabs := sdk.Row("tabs",
		p.tabButton("tab-playlists", locales.SpotifyTabPlaylists(), s.tab == tabPlaylists),
		p.tabButton("tab-albums", locales.SpotifyTabAlbums(), s.tab == tabAlbums),
		sdk.Spacer("tabs-space"),
		sdk.IconButton("tab-search", "search").Label(locales.SpotifyTabSearch()).Active(s.tab == tabSearch),
		sdk.IconButton("tab-queue", "list").Label(locales.SpotifyTabQueue()).Active(s.tab == tabQueue),
	).Gap(8)

	content := p.libraryContent(s)
	return sdk.Column("library-content", tabs, content).Gap(8).Grow(1)
}

// drillTree is the tracks of one playlist or album: a back button, the
// name, a Play button for the whole list, and the rows.
func (p *plugin) drillTree(s *session) *sdk.Node {
	head := sdk.Row("drill-head",
		sdk.IconButton("drill-back", "back"),
		sdk.Text("drill-name", s.drillName).Role(protocol.RoleTitle).Lines(1).Grow(1),
		sdk.Button("drill-play", locales.SpotifyPlay()).Primary(),
	).Gap(8)
	var body *sdk.Node
	switch {
	case s.tracksState == tracksForbidden:
		lines := []*sdk.Node{sdk.Text("tracks-forbidden", locales.SpotifyTracksForbidden()).Role(protocol.RoleCaption).Lines(2)}
		if s.needScope {
			lines = append(lines,
				sdk.Text("tracks-scope", locales.SpotifyTracksScope()).Role(protocol.RoleCaption).Lines(3),
				sdk.Button("sign-in-again", locales.SpotifySignInAgain()),
			)
		}
		body = sdk.Column("tracks-note", lines...).Gap(8)
	case s.tracksState == tracksFailed && len(s.tracks) == 0:
		body = sdk.EmptyState("state-tracks-failed", locales.SpotifyTracksFailed(), locales.SpotifyRetryDetail()).Action(locales.SpotifyRetry()).Grow(1)
	case s.tracksState == tracksLoading && len(s.tracks) == 0:
		body = sdk.EmptyState("state-loading", locales.SpotifyLibraryLoading(), "").Grow(1)
	case len(s.tracks) == 0:
		body = sdk.EmptyState("state-no-tracks", locales.SpotifyNoTracks(), locales.SpotifyNoTracksDetail()).Grow(1)
	default:
		body = sdk.List("tracks", trackItems(s.tracks, "t-", s.playingURI())...).Grow(1)
	}
	return sdk.Column("library-content", head, body).Gap(8).Grow(1)
}

// queueContent is the tracks that come next, or its empty state.
func (p *plugin) queueContent(s *session) *sdk.Node {
	switch {
	case len(s.queue) > 0:
		return sdk.List("queue", trackItems(s.queue, "q-", "")...).Grow(1)
	case !s.queueLoaded:
		return sdk.EmptyState("state-loading", locales.SpotifyLibraryLoading(), "").Grow(1)
	case s.queueFailed:
		return sdk.EmptyState("state-tracks-failed", locales.SpotifyTracksFailed(), locales.SpotifyRetryDetail()).Action(locales.SpotifyRetry()).Grow(1)
	}
	return sdk.EmptyState("state-queue-empty", locales.SpotifyQueueEmpty(), locales.SpotifyQueueEmptyDetail()).Grow(1)
}

// playingURI is the track the player holds now, or "" when none plays or is
// held.
func (s *session) playingURI() string {
	if s.playback != modePlaying || s.snap.State == transport.StateStopped {
		return ""
	}
	return s.snap.URI
}

// trackItems are the rows of a track list: the number, the title, the
// artists and the length. A row's id is its place in the list, because a
// playlist may hold a track twice; the track that plays is marked.
func trackItems(tracks []web.ListTrack, prefix, playing string) []protocol.Item {
	items := make([]protocol.Item, 0, len(tracks))
	for i, track := range tracks {
		title := track.Name
		if title == "" {
			title = locales.SpotifyUntitledTrack()
		}
		items = append(items, protocol.Item{
			ID:       prefix + strconv.Itoa(i),
			Title:    title,
			Subtitle: track.ArtistLine(),
			Trailing: clock(time.Duration(track.DurationMS) * time.Millisecond),
			Leading:  strconv.Itoa(i + 1),
			Active:   playing != "" && track.URI == playing,
		})
	}
	return items
}

// libraryContent draws the state of the Web API side, or the tab's rows.
func (p *plugin) libraryContent(s *session) *sdk.Node {
	if s.signingIn && s.lib != libReady {
		return sdk.EmptyState("state-signing-in", locales.SpotifySignInRunning(), "").Grow(1)
	}
	switch s.lib {
	case libNoClientID:
		return sdk.EmptyState("setup", locales.SpotifyNoClientID(), locales.SpotifyNoClientIDDetail(p.redirectURI())).
			Action(locales.SpotifySetUp()).Grow(1)
	case libSignedOut:
		return sdk.EmptyState("sign-in", locales.SpotifySignedOut(), locales.SpotifySignedOutDetail()).
			Action(locales.SpotifySignIn()).Grow(1)
	case libTokenRefused:
		return sdk.EmptyState("sign-in-again", locales.SpotifyTokenRefused(), locales.SpotifyTokenRefusedDetail()).
			Action(locales.SpotifySignInAgain()).Grow(1)
	case libPremiumLapsed:
		return sdk.EmptyState("state-premium", locales.SpotifyPremium(), locales.SpotifyPremiumDetail()).Grow(1)
	case libFailed:
		return sdk.EmptyState("state-library-failed", locales.SpotifyLibraryFailed(), locales.SpotifyRetryDetail()).Action(locales.SpotifyRetry()).Grow(1)
	}

	if s.loading && len(s.entries) == 0 && s.tab != tabQueue {
		return sdk.EmptyState("state-loading", locales.SpotifyLibraryLoading(), "").Grow(1)
	}
	if s.lib == libNetworkDown && len(s.entries) == 0 && s.tab != tabQueue {
		return sdk.EmptyState("state-offline", locales.SpotifyOffline(), locales.SpotifyRetryDetail()).Action(locales.SpotifyRetry()).Grow(1)
	}
	if s.lib == libRateLimited && len(s.entries) == 0 && s.tab != tabQueue {
		return sdk.EmptyState("state-rate-limited", locales.SpotifyRateLimitedTitle(), locales.SpotifyRateLimitedDetail()).Grow(1)
	}

	children := make([]*sdk.Node, 0, 3)
	switch s.lib {
	case libNetworkDown:
		children = append(children, sdk.Text("offline", locales.SpotifyOffline()).Role(protocol.RoleCaption).Color(protocol.ColorAccent).Lines(1))
	case libRateLimited:
		children = append(children, sdk.Text("rate-limited", locales.SpotifyRateLimited()).Role(protocol.RoleCaption).Color(protocol.ColorAccent).Lines(1))
	}
	switch s.tab {
	case tabSearch:
		// The field shows the saved query after a restart; the host
		// applies it when the field first shows, or when it changes here.
		children = append(children, sdk.SearchField("search", locales.SpotifySearchPlaceholder()).Query(s.query))
		children = append(children, p.searchContent(s))
	case tabPlaylists:
		children = append(children, p.listContent(s, locales.SpotifyNoPlaylists(), locales.SpotifyNoPlaylistsDetail()))
	case tabAlbums:
		children = append(children, p.listContent(s, locales.SpotifyNoAlbums(), locales.SpotifyNoAlbumsDetail()))
	case tabQueue:
		children = append(children, p.queueContent(s))
	}
	return sdk.Column("tab-content", children...).Gap(6).Grow(1)
}

// searchContent is the prompt, the results, or the no-results state of
// the search tab.
func (p *plugin) searchContent(s *session) *sdk.Node {
	if len(s.entries) > 0 {
		return p.listNode(s)
	}
	if s.query == "" {
		return sdk.EmptyState("state-search-prompt", locales.SpotifySearchPrompt(), locales.SpotifySearchPromptDetail()).Grow(1)
	}
	return sdk.EmptyState("state-no-results", locales.SpotifyNoResults(), locales.SpotifyNoResultsDetail()).Grow(1)
}

// listContent is a tab's rows, or its empty state.
func (p *plugin) listContent(s *session, title, detail string) *sdk.Node {
	if len(s.entries) > 0 {
		return p.listNode(s)
	}
	return sdk.EmptyState("state-empty-list", title, detail).Grow(1)
}

// listNode turns the rows of a session into a grid of tiles, as many to
// a row as the pane has room for.
func (p *plugin) listNode(s *session) *sdk.Node {
	tile, _ := gridFit(s.size.Width)
	limit := s.tileLimit()
	items := s.items(limit)
	if len(s.entries) > limit {
		// A Show More tile at the end reaches the rows past the cap.
		items = append(items, protocol.Item{ID: moreID, Title: locales.SpotifyShowMore()})
	}
	return sdk.List("results", items...).Grid(tile).Grow(1)
}

// tileLimit is how many rows of the library the grid draws: maxTiles, and
// another maxTiles for each press on Show More since the tab opened.
func (s *session) tileLimit() int {
	return max(s.shownTiles, maxTiles)
}

// items is the first n rows of a session as list items.
func (s *session) items(n int) []protocol.Item {
	n = min(n, len(s.entries))
	items := make([]protocol.Item, 0, n)
	for _, entry := range s.entries[:n] {
		items = append(items, protocol.Item{
			ID:       entry.id,
			Title:    entry.title,
			Subtitle: entry.subtitle,
			Trailing: entry.trailing,
			Image:    entry.image,
		})
	}
	return items
}

// tabButton is one tab: a primary button when it is the open tab.
func (p *plugin) tabButton(id, label string, active bool) *sdk.Node {
	button := sdk.Button(id, label)
	if active {
		button.Primary()
	}
	return button
}

// artNode is the album art, or nil when the track has no art. While the
// track plays the art tints the pane and the app with its own color; a
// paused or stopped track sends no tint, so the color fades out.
func (s *session) artNode(id string, size, radius int) *sdk.Node {
	if s.snap.ArtworkURL == "" {
		return nil
	}
	art := sdk.Image(id, s.snap.ArtworkURL).Size(size, size).Radius(radius)
	if s.snap.State == transport.StatePlaying {
		art.Tint()
	}
	return art
}

// rowNodes is a row that leaves out a part that has nothing to draw, such
// as the art of a track without a picture.
func rowNodes(id string, children ...*sdk.Node) *sdk.Node {
	return sdk.Row(id, kept(children)...)
}

// columnNodes is a column that leaves out a part that has nothing to
// draw, such as the notice when no command failed.
func columnNodes(id string, children ...*sdk.Node) *sdk.Node {
	return sdk.Column(id, kept(children)...)
}

// kept is the parts that are not nil.
func kept(children []*sdk.Node) []*sdk.Node {
	out := make([]*sdk.Node, 0, len(children))
	for _, child := range children {
		if child != nil {
			out = append(out, child)
		}
	}
	return out
}

// redirectURI is the redirect URI the user registers in the Spotify
// dashboard: the client's, or the default one before there is a client.
func (p *plugin) redirectURI() string {
	if p.library != nil {
		return p.library.RedirectURI()
	}
	return web.DefaultRedirectURI()
}

// playIcon is pause while a track plays, and play otherwise.
func (s *session) playIcon() string {
	if s.snap.State == transport.StatePlaying {
		return "pause"
	}
	return "play"
}

// position is where the track is by now: the snapshot's position, and the
// time since the snapshot while it plays. It reads the clock, so it is for
// a handler that makes a new snapshot, never for a tree.
func (s *session) position() time.Duration {
	position := s.snap.Position
	if s.snap.State == transport.StatePlaying && !s.snap.ReadAt.IsZero() {
		position += time.Since(s.snap.ReadAt)
	}
	if s.snap.Duration > 0 && position > s.snap.Duration {
		position = s.snap.Duration
	}
	if position < 0 {
		position = 0
	}
	return position
}

// fraction is the position as a share of the duration, 0 when there is
// no duration. Only a track that does not play uses it for a tree, so it
// holds still.
func (s *session) fraction() float64 {
	if s.snap.Duration <= 0 {
		return 0
	}
	fraction := float64(s.position()) / float64(s.snap.Duration)
	if fraction < 0 {
		return 0
	}
	if fraction > 1 {
		return 1
	}
	return fraction
}

// clock writes a duration as m:ss, or h:mm:ss from an hour on.
func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second) / time.Second)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	seconds %= 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
