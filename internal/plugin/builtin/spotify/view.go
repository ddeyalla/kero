package spotify

import (
	"fmt"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/locales"
)

// The pane's messages. They are kept as values so that the tests and the
// code below name them once.
var (
	localeCommandFailed = locales.SpotifyCommandFailed
	localeSeekFailed    = locales.SpotifySeekFailed
	localeVolumeFailed  = locales.SpotifyVolumeFailed
	localePlayFailed    = locales.SpotifyPlayFailed
	localeSignInRunning = locales.SpotifySignInRunning
	localeSignInFailed  = locales.SpotifySignInFailed
	localeNoClientID    = locales.SpotifyNoClientID
)

// tree builds the pane: the compact player, or the large player with the
// library.
func (p *plugin) tree(s *session) protocol.Tree {
	if compact(s.size) {
		return p.compactTree(s).Tree()
	}
	return p.largeTree(s).Tree()
}

// compactTree is the 280x150 player: the art, the title and the artist,
// the controls, and a thin progress bar.
func (p *plugin) compactTree(s *session) *sdk.Node {
	if s.playback != modePlaying {
		return sdk.Column(widgetPlayer, p.playbackState(s)).Gap(4).Padding(8)
	}

	children := make([]*sdk.Node, 0, 5)
	art := s.artNode("art", 40, 6)
	title := sdk.Text("title", s.snap.Track).Role(protocol.RoleTitle).Lines(1).Marquee()
	artist := sdk.Text("artist", s.snap.Artist).Role(protocol.RoleCaption).Lines(1)
	meta := sdk.Column("meta", title, artist).Gap(2).Grow(1)
	children = append(children, rowNodes("head", art, meta).Gap(8))

	prev := sdk.IconButton("prev", "previous")
	play := sdk.IconButton("play", s.playIcon())
	next := sdk.IconButton("next", "next")
	children = append(children, sdk.Row("controls", prev, play, next, sdk.Spacer("controls-space")).Gap(4))

	progress := sdk.Progress("progress", s.fraction())
	if s.snap.State == transport.StatePlaying {
		progress.Playing(s.snap.Rate(), s.snap.At())
	}
	children = append(children, progress)
	if s.notice != "" {
		children = append(children, sdk.Text("notice", s.notice).Role(protocol.RoleCaption).Color(protocol.ColorAccent).Lines(2))
	}

	return sdk.Column(widgetPlayer, children...).Gap(8).Padding(12)
}

// largeTree is the large player: the art and the track, a seekable
// progress bar, the controls with the volume, and the library tabs with
// the search field and the results.
func (p *plugin) largeTree(s *session) *sdk.Node {
	if s.playback != modePlaying {
		return sdk.Column(widgetPlayer, p.playbackState(s)).Gap(8).Padding(12)
	}

	art := s.artNode("art", 96, 8)
	title := sdk.Text("title", s.snap.Track).Role(protocol.RoleTitle).Lines(1).Marquee()
	artist := sdk.Text("artist", s.snap.Artist).Role(protocol.RoleCaption).Lines(1)
	meta := []*sdk.Node{title, artist}
	if s.snap.Album != "" {
		meta = append(meta, sdk.Text("album", s.snap.Album).Role(protocol.RoleCaption).Lines(1))
	}
	header := rowNodes("head", art, sdk.Column("meta", meta...).Gap(2).Grow(1)).Gap(12)

	seek := sdk.Slider("seek", s.fraction(), 0, 1).Step(0.001)
	times := sdk.Row("times",
		sdk.Text("elapsed", clock(s.position())).Role(protocol.RoleCaption),
		sdk.Spacer("times-space"),
		sdk.Text("duration", clock(s.snap.Duration)).Role(protocol.RoleCaption),
	).Gap(4)

	prev := sdk.IconButton("prev", "previous")
	play := sdk.IconButton("play", s.playIcon())
	next := sdk.IconButton("next", "next")
	shuffle := sdk.Button("shuffle", locales.SpotifyShuffle())
	if s.snap.Shuffle {
		shuffle.Primary()
	}
	repeat := sdk.Button("repeat", locales.SpotifyRepeat())
	if s.snap.Repeat {
		repeat.Primary()
	}
	controls := sdk.Row("controls", prev, play, next, sdk.Spacer("controls-space"), shuffle, repeat).Gap(6)

	volumeLabel := sdk.Text("volume-label", locales.SpotifyVolume()).Role(protocol.RoleCaption)
	volume := sdk.Slider("volume", float64(s.volumeValue()), 0, 100).Step(1).Grow(1)
	volumeRow := sdk.Row("volume-row", volumeLabel, volume).Gap(8)

	children := []*sdk.Node{header, seek, times, controls, volumeRow}
	if s.notice != "" {
		children = append(children, sdk.Text("notice", s.notice).Role(protocol.RoleCaption).Color(protocol.ColorAccent).Lines(2))
	}
	children = append(children, sdk.Divider("library"))
	children = append(children, p.libraryTree(s))
	return sdk.Column(widgetPlayer, children...).Gap(8).Padding(12)
}

// playbackState is the empty state of a player that cannot play, or has
// nothing to play. Its node id is the action it offers, so a press is
// dispatched without comparing a translated label.
func (p *plugin) playbackState(s *session) *sdk.Node {
	switch s.playback {
	case modeNotRunning:
		return sdk.EmptyState("open-spotify", locales.SpotifyNotRunningTitle(), locales.SpotifyNotRunningDetail()).
			Action(locales.SpotifyOpen())
	case modeAutomation:
		return sdk.EmptyState("privacy", locales.SpotifyAutomationTitle(), locales.SpotifyAutomationDetail()).
			Action(locales.SpotifyAutomationButton())
	case modeUnavailable:
		return sdk.EmptyState("state-unavailable", locales.SpotifyUnavailable(), locales.SpotifyUnavailableDetail())
	case modeIdle:
		return sdk.EmptyState("state-idle", locales.SpotifyNothingPlaying(), locales.SpotifyNothingPlayingDetail())
	default:
		return sdk.EmptyState("state-starting", locales.SpotifyStarting(), "")
	}
}

// libraryTree is the tabs and the library content. A library state that
// is not ready replaces the content, but the tabs stay so that a press
// still explains the sign-in that is missing.
func (p *plugin) libraryTree(s *session) *sdk.Node {
	tabs := sdk.Row("tabs",
		p.tabButton("tab-search", locales.SpotifyTabSearch(), s.tab == tabSearch),
		p.tabButton("tab-playlists", locales.SpotifyTabPlaylists(), s.tab == tabPlaylists),
		p.tabButton("tab-albums", locales.SpotifyTabAlbums(), s.tab == tabAlbums),
	).Gap(6)

	content := p.libraryContent(s)
	return sdk.Column("library-content", tabs, content).Gap(8).Grow(1)
}

// libraryContent draws the state of the Web API side, or the tab's rows.
func (p *plugin) libraryContent(s *session) *sdk.Node {
	if s.signingIn && s.lib != libReady {
		return sdk.EmptyState("state-signing-in", locales.SpotifySignInRunning(), "").Grow(1)
	}
	switch s.lib {
	case libNoClientID:
		return sdk.EmptyState("setup", locales.SpotifyNoClientID(), locales.SpotifyNoClientIDDetail()).
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
		return sdk.EmptyState("state-library-failed", locales.SpotifyLibraryFailed(), "").Grow(1)
	}

	if s.loading && len(s.entries) == 0 {
		return sdk.EmptyState("state-loading", locales.SpotifyLibraryLoading(), "").Grow(1)
	}
	if s.lib == libNetworkDown && len(s.entries) == 0 {
		return sdk.EmptyState("state-offline", locales.SpotifyOffline(), locales.SpotifyLibraryFailed()).Grow(1)
	}

	children := make([]*sdk.Node, 0, 3)
	if s.lib == libNetworkDown {
		children = append(children, sdk.Text("offline", locales.SpotifyOffline()).Role(protocol.RoleCaption).Color(protocol.ColorAccent).Lines(1))
	}
	switch s.tab {
	case tabSearch:
		children = append(children, sdk.SearchField("search", locales.SpotifySearchPlaceholder()))
		children = append(children, p.searchContent(s))
	case tabPlaylists:
		children = append(children, p.listContent(s, locales.SpotifyNoPlaylists(), locales.SpotifyNoPlaylistsDetail()))
	case tabAlbums:
		children = append(children, p.listContent(s, locales.SpotifyNoAlbums(), locales.SpotifyNoAlbumsDetail()))
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

// listNode turns the rows of a session into a list part.
func (p *plugin) listNode(s *session) *sdk.Node {
	items := make([]protocol.Item, 0, len(s.entries))
	for _, entry := range s.entries {
		items = append(items, protocol.Item{
			ID:       entry.id,
			Title:    entry.title,
			Subtitle: entry.subtitle,
			Trailing: entry.trailing,
			Image:    entry.image,
		})
	}
	return sdk.List("results", items...).Grow(1)
}

// tabButton is one tab: a primary button when it is the open tab.
func (p *plugin) tabButton(id, label string, active bool) *sdk.Node {
	button := sdk.Button(id, label)
	if active {
		button.Primary()
	}
	return button
}

// artNode is the album art, tinted with its own color, or nil when the
// track has no art.
func (s *session) artNode(id string, size, radius int) *sdk.Node {
	if s.snap.ArtworkURL == "" {
		return nil
	}
	return sdk.Image(id, s.snap.ArtworkURL).Size(size, size).Radius(radius).Tint()
}

// rowNodes is a row that leaves out a part that has nothing to draw, such
// as the art of a track without a picture.
func rowNodes(id string, children ...*sdk.Node) *sdk.Node {
	kept := make([]*sdk.Node, 0, len(children))
	for _, child := range children {
		if child != nil {
			kept = append(kept, child)
		}
	}
	return sdk.Row(id, kept...)
}

// playIcon is pause while a track plays, and play otherwise.
func (s *session) playIcon() string {
	if s.snap.State == transport.StatePlaying {
		return "pause"
	}
	return "play"
}

// position is where the track is, including the seek preview while the
// slider is dragged, or the time since the snapshot while it plays.
func (s *session) position() time.Duration {
	if s.seek != nil {
		return *s.seek
	}
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
// no duration.
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

// volumeValue is the volume, or its preview while the slider is dragged.
func (s *session) volumeValue() int {
	if s.volume != nil {
		return *s.volume
	}
	return s.snap.Volume
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
