// Package transport talks to the Spotify desktop app on macOS through
// AppleScript, and listens for Spotify's own playback notifications.
//
// A Transport runs one AppleScript at a time, each with a one second
// timeout, and reads the whole player in a single call. A command waits
// for the call in flight; a read that finds one is dropped. Every script
// but Launch first checks that Spotify is running, so only the player's
// Open Spotify button starts the app.
// macOS can refuse the Apple events; a Transport reports that as
// [ErrAutomation] with the way to allow it, and a missing app as
// [ErrNotRunning], so callers do not parse message strings.
//
// A Monitor reads only while a pane shows the player. It reads about
// once a second, or once every ten seconds while a [Source] listens for
// Spotify's com.spotify.client.PlaybackStateChanged notification and
// reads at once on each change.
package transport

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout is how long one AppleScript may take.
const DefaultTimeout = time.Second

// notRunningToken is what every script returns when Spotify is not
// running. It is a token rather than a sentence so that a read and a
// command classify it the same way.
const notRunningToken = "not-running"

// fieldSeparator separates the fields of a read. It is the ASCII unit
// separator, which a track name, an artist, an album or a URL does not
// contain.
const fieldSeparator = "\x1f"

var (
	// ErrNotRunning says the Spotify app was not running.
	ErrNotRunning = errors.New("spotify is not running")

	// ErrAutomation says macOS refused to let this app control Spotify.
	// The user allows it in System Settings, Privacy & Security,
	// Automation.
	ErrAutomation = errors.New("spotify refused Apple events: allow this app to control Spotify in System Settings > Privacy & Security > Automation")

	// ErrTimeout says Spotify did not answer within the transport's
	// timeout.
	ErrTimeout = errors.New("spotify did not answer in time")

	// ErrBusy says a read could not start because an earlier call is
	// still running. A read is dropped rather than queued; a command
	// waits for the call in flight.
	ErrBusy = errors.New("spotify is still answering an earlier call")

	// ErrRange says a value lies outside what Spotify accepts.
	ErrRange = errors.New("value out of range")

	// ErrURI says a string is not a Spotify URI.
	ErrURI = errors.New("not a Spotify URI")

	// ErrOutput says osascript answered with something a read did not
	// expect.
	ErrOutput = errors.New("unexpected osascript answer")
)

// State is what Spotify's player is doing.
type State int

const (
	// StateStopped means no track is playing.
	StateStopped State = iota
	// StatePlaying means a track plays.
	StatePlaying
	// StatePaused means a track is loaded and paused.
	StatePaused
)

// String returns the name Spotify's AppleScript uses.
func (s State) String() string {
	switch s {
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	default:
		return "stopped"
	}
}

// Snapshot is what the player was doing when the transport read it.
type Snapshot struct {
	State      State
	Track      string
	Artist     string
	Album      string
	ArtworkURL string
	Duration   time.Duration
	Position   time.Duration
	Volume     int
	Shuffle    bool
	Repeat     bool
	ReadAt     time.Time
}

// Fraction is how much of the track had played when the snapshot was
// read, from 0 to 1. It is 0 when the duration is unknown.
func (s Snapshot) Fraction() float64 {
	if s.Duration <= 0 {
		return 0
	}
	fraction := float64(s.Position) / float64(s.Duration)
	if fraction < 0 {
		return 0
	}
	if fraction > 1 {
		return 1
	}
	return fraction
}

// Rate is how much of the track plays each second, from 0 to 1, and 0
// while the track is not playing. A host draws a progress bar from
// Fraction and lets it advance by Rate from At.
func (s Snapshot) Rate() float64 {
	if s.State != StatePlaying || s.Duration <= 0 {
		return 0
	}
	return 1 / s.Duration.Seconds()
}

// At is when the bar was at Fraction, in Unix milliseconds, for a host
// that advances the bar itself.
func (s Snapshot) At() int64 {
	return s.ReadAt.UnixMilli()
}

// Transport reads the Spotify player and sends it commands. It runs
// AppleScripts through its Runner, one call at a time.
type Transport struct {
	// Runner runs the AppleScripts. It is required.
	Runner Runner

	// Timeout is how long one call may take. Zero means
	// DefaultTimeout.
	Timeout time.Duration

	// slot holds the one call. It is a channel rather than a mutex so
	// that a command can wait for the call in flight within its context.
	once sync.Once
	slot chan struct{}
	now  func() time.Time
}

// New returns a Transport that runs its scripts with r.
func New(r Runner) *Transport {
	return &Transport{Runner: r}
}

func (t *Transport) timeNow() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// call runs one script and classifies its result. Only one call holds the
// slot at a time. A command waits for it until its context ends; a read
// takes the slot only when free and yields [ErrBusy] otherwise, so a
// pending command can never be starved by polling.
func (t *Transport) call(ctx context.Context, script string, wait bool) (string, error) {
	if t.Runner == nil {
		return "", errors.New("transport: no runner")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := t.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := t.acquire(runCtx, wait); err != nil {
		if errors.Is(err, ErrBusy) {
			return "", ErrBusy
		}
		return "", commandError(runCtx, err)
	}
	defer t.release()

	out, err := t.Runner.Run(runCtx, script)
	if err != nil {
		return "", commandError(runCtx, err)
	}
	out = strings.TrimSpace(out)
	if out == notRunningToken {
		return "", ErrNotRunning
	}
	return out, nil
}

// slotOf returns the one call slot.
func (t *Transport) slotOf() chan struct{} {
	t.once.Do(func() {
		t.slot = make(chan struct{}, 1)
	})
	return t.slot
}

// acquire takes the one call slot. A command waits for it until its
// context ends; a read takes it only if it is free.
func (t *Transport) acquire(ctx context.Context, wait bool) error {
	slot := t.slotOf()
	if !wait {
		select {
		case slot <- struct{}{}:
			return nil
		default:
			return ErrBusy
		}
	}
	select {
	case slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release returns the slot taken by acquire.
func (t *Transport) release() {
	<-t.slotOf()
}

// commandError turns a Runner failure into one of the typed errors. The
// reason is only in osascript's standard error, so the classification
// reads it.
func commandError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrTimeout, context.DeadlineExceeded)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	var commandErr *CommandError
	text := err.Error()
	if errors.As(err, &commandErr) && commandErr.Stderr != "" {
		text = commandErr.Stderr
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(text, "-1743"),
		strings.Contains(lower, "not authorized to send apple events"),
		strings.Contains(lower, "not allowed to send apple events"),
		strings.Contains(lower, "erraeeventnotpermitted"):
		return fmt.Errorf("%w: %s", ErrAutomation, strings.TrimSpace(text))
	case strings.Contains(text, "-600"),
		strings.Contains(lower, "isn't running"),
		strings.Contains(lower, "is not running"):
		return fmt.Errorf("%w: %s", ErrNotRunning, strings.TrimSpace(text))
	}
	return err
}

// run runs a command script and drops its output. Commands wait for the
// call in flight, so a pane action is not lost to a poll.
func (t *Transport) run(ctx context.Context, script string) error {
	_, err := t.call(ctx, script, true)
	return err
}

// commandScript puts the running guard at the top of a command script.
// A body is a tell statement, so the guard can answer the not-running
// token before Spotify is touched.
func commandScript(body string) string {
	return "if application \"Spotify\" is running then\n" + body + "\nelse\n\treturn \"" + notRunningToken + "\"\nend if"
}

// PlayPause toggles between playing and paused.
func (t *Transport) PlayPause(ctx context.Context) error {
	return t.run(ctx, commandScript(`tell application "Spotify" to playpause`))
}

// Next skips to the next track.
func (t *Transport) Next(ctx context.Context) error {
	return t.run(ctx, commandScript(`tell application "Spotify" to next track`))
}

// Previous goes back to the previous track.
func (t *Transport) Previous(ctx context.Context) error {
	return t.run(ctx, commandScript(`tell application "Spotify" to previous track`))
}

// Launch brings the Spotify app to the front, and starts it when it is
// not running. Every other script refuses to start the app, so this is
// the one command the player offers when Spotify is closed.
func (t *Transport) Launch(ctx context.Context) error {
	return t.run(ctx, `tell application "Spotify" to activate`)
}

// Seek moves within the current track.
func (t *Transport) Seek(ctx context.Context, position time.Duration) error {
	if position < 0 {
		return fmt.Errorf("%w: position %s", ErrRange, position)
	}
	seconds := strconv.FormatFloat(position.Seconds(), 'f', 3, 64)
	return t.run(ctx, commandScript(`tell application "Spotify" to set player position to `+seconds))
}

// Volume sets Spotify's own volume, from 0 to 100. It does not touch
// the volume of the Mac.
func (t *Transport) Volume(ctx context.Context, volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("%w: volume %d", ErrRange, volume)
	}
	return t.run(ctx, commandScript(`tell application "Spotify" to set sound volume to `+strconv.Itoa(volume)))
}

// Shuffle turns shuffle on or off.
func (t *Transport) Shuffle(ctx context.Context, on bool) error {
	return t.run(ctx, commandScript(`tell application "Spotify" to set shuffling to `+strconv.FormatBool(on)))
}

// Repeat turns repeat on or off.
func (t *Transport) Repeat(ctx context.Context, on bool) error {
	return t.run(ctx, commandScript(`tell application "Spotify" to set repeating to `+strconv.FormatBool(on)))
}

// Play starts playing a Spotify URI, such as spotify:album:... or
// spotify:playlist:...
func (t *Transport) Play(ctx context.Context, uri string) error {
	if err := checkURI(uri); err != nil {
		return err
	}
	return t.run(ctx, commandScript(`tell application "Spotify" to play track "`+uri+`"`))
}

// checkURI rejects a string that is not a Spotify URI before it reaches
// an AppleScript, where a quote could end the string early.
func checkURI(uri string) error {
	if !strings.HasPrefix(uri, "spotify:") || len(uri) == len("spotify:") {
		return fmt.Errorf("%w: %q", ErrURI, uri)
	}
	if strings.ContainsAny(uri, "\"\\\n\r") {
		return fmt.Errorf("%w: %q holds a quote or a newline", ErrURI, uri)
	}
	return nil
}

// Read returns the player in one AppleScript call.
func (t *Transport) Read(ctx context.Context) (Snapshot, error) {
	out, err := t.call(ctx, readScript, false)
	if err != nil {
		return Snapshot{}, err
	}
	return parseSnapshot(out, t.timeNow())
}

// readScript reads every field of the player. The track fields are
// optional, because Spotify keeps no track while it is stopped; the
// other fields are always readable. Spotify reports the duration in
// milliseconds and the position in seconds.
const readScript = `if application "Spotify" is running then
tell application "Spotify"
	set d to (ASCII character 31)
	set stateText to "stopped"
	try
		set stateText to player state as text
	end try
	set trackName to ""
	set trackArtist to ""
	set trackAlbum to ""
	set trackArtwork to ""
	set trackDuration to 0
	try
		set trackName to name of current track
		set trackArtist to artist of current track
		set trackAlbum to album of current track
		set trackArtwork to artwork url of current track
		set trackDuration to duration of current track
	end try
	return stateText & d & trackName & d & trackArtist & d & trackAlbum & d & trackArtwork & d & (trackDuration as text) & d & (player position as text) & d & (sound volume as text) & d & (shuffling as text) & d & (repeating as text)
end tell
else
	return "not-running"
end if`

// parseSnapshot turns the fields of a read into a Snapshot.
func parseSnapshot(out string, readAt time.Time) (Snapshot, error) {
	fields := strings.Split(out, fieldSeparator)
	if len(fields) != 10 {
		return Snapshot{}, fmt.Errorf("%w: %d fields, want 10", ErrOutput, len(fields))
	}

	duration, err := parseNumber("duration", fields[5])
	if err != nil {
		return Snapshot{}, err
	}
	position, err := parseNumber("position", fields[6])
	if err != nil {
		return Snapshot{}, err
	}
	volume, err := strconv.Atoi(strings.TrimSpace(fields[7]))
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: volume %q", ErrOutput, fields[7])
	}

	return Snapshot{
		State:      parseState(fields[0]),
		Track:      fields[1],
		Artist:     fields[2],
		Album:      fields[3],
		ArtworkURL: fields[4],
		Duration:   time.Duration(duration * float64(time.Millisecond)),
		Position:   time.Duration(position * float64(time.Second)),
		Volume:     volume,
		Shuffle:    parseBool(fields[8]),
		Repeat:     parseBool(fields[9]),
		ReadAt:     readAt,
	}, nil
}

func parseNumber(name, text string) (float64, error) {
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %q", ErrOutput, name, text)
	}
	return number, nil
}

func parseState(text string) State {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "playing":
		return StatePlaying
	case "paused":
		return StatePaused
	default:
		return StateStopped
	}
}

func parseBool(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "true")
}
