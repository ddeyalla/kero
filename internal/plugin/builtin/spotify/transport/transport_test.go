package transport

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner is a Runner that records the scripts it receives and
// answers with what a test sets. Tests never run a real osascript.
type fakeRunner struct {
	mu      sync.Mutex
	scripts []string
	run     func(ctx context.Context, script string) (string, error)
}

// Run records the script and answers it.
func (f *fakeRunner) Run(ctx context.Context, script string) (string, error) {
	f.mu.Lock()
	f.scripts = append(f.scripts, script)
	run := f.run
	f.mu.Unlock()
	if run != nil {
		return run(ctx, script)
	}
	return "", nil
}

// calls returns the scripts the runner ran so far.
func (f *fakeRunner) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.scripts...)
}

// guarded returns the script commandScript makes for a body, written
// out again so that a test does not lean on the code it tests.
func guarded(body string) string {
	return "if application \"Spotify\" is running then\n" + body + "\nelse\n\treturn \"not-running\"\nend if"
}

// readOutput joins the fields of a read script's output the way the
// script does.
func readOutput(fields ...string) string {
	return strings.Join(fields, fieldSeparator) + "\n"
}

// playingOutput is what a read returns while a track plays.
func playingOutput() string {
	return readOutput("playing", "Flugufrelsarinn", "Sigur Rós", "Ágætis byrjun",
		"https://i.scdn.co/image/abc", "467933", "377.52099609375", "100", "true", "false")
}

func TestReadMapsEveryField(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return playingOutput(), nil
	}}
	player := New(runner)
	readAt := time.UnixMilli(1_700_000_000_000)
	player.now = func() time.Time { return readAt }

	snapshot, err := player.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if snapshot.State != StatePlaying {
		t.Errorf("State = %v, want playing", snapshot.State)
	}
	if snapshot.Track != "Flugufrelsarinn" || snapshot.Artist != "Sigur Rós" ||
		snapshot.Album != "Ágætis byrjun" || snapshot.ArtworkURL != "https://i.scdn.co/image/abc" {
		t.Errorf("track fields = %+v", snapshot)
	}
	if want := 467933 * time.Millisecond; snapshot.Duration != want {
		t.Errorf("Duration = %v, want %v (Spotify reports milliseconds)", snapshot.Duration, want)
	}
	positionSeconds := 377.52099609375
	if want := time.Duration(positionSeconds * float64(time.Second)); snapshot.Position != want {
		t.Errorf("Position = %v, want %v", snapshot.Position, want)
	}
	if snapshot.Volume != 100 || !snapshot.Shuffle || snapshot.Repeat {
		t.Errorf("Volume/Shuffle/Repeat = %d/%v/%v, want 100/true/false",
			snapshot.Volume, snapshot.Shuffle, snapshot.Repeat)
	}
	if !snapshot.ReadAt.Equal(readAt) {
		t.Errorf("ReadAt = %v, want %v", snapshot.ReadAt, readAt)
	}

	calls := runner.calls()
	if len(calls) != 1 {
		t.Fatalf("ran %d scripts, want 1", len(calls))
	}
	if !strings.HasPrefix(calls[0], "if application \"Spotify\" is running then") {
		t.Errorf("read script does not start with the running guard:\n%s", calls[0])
	}
}

func TestReadStoppedPlayer(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return readOutput("stopped", "", "", "", "", "0", "0", "0", "false", "false"), nil
	}}
	player := New(runner)

	snapshot, err := player.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if snapshot.State != StateStopped {
		t.Errorf("State = %v, want stopped", snapshot.State)
	}
	if snapshot.Rate() != 0 || snapshot.Fraction() != 0 {
		t.Errorf("Rate/Fraction = %v/%v, want 0/0", snapshot.Rate(), snapshot.Fraction())
	}
}

func TestReadRejectsUnexpectedOutput(t *testing.T) {
	fields := map[string]string{
		"too few fields":        readOutput("playing", "a", "b"),
		"duration not a number": readOutput("playing", "a", "b", "c", "d", "soon", "1", "1", "false", "false"),
		"position not a number": readOutput("playing", "a", "b", "c", "d", "1", "soon", "1", "false", "false"),
		"volume not a number":   readOutput("playing", "a", "b", "c", "d", "1", "1", "loud", "false", "false"),
	}
	for name, output := range fields {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{run: func(context.Context, string) (string, error) {
				return output, nil
			}}
			if _, err := New(runner).Read(context.Background()); !errors.Is(err, ErrOutput) {
				t.Fatalf("Read error = %v, want ErrOutput", err)
			}
		})
	}
}

func TestCommandsUseTheirScripts(t *testing.T) {
	tests := []struct {
		name string
		body string
		call func(context.Context, *Transport) error
	}{
		{"playpause", `tell application "Spotify" to playpause`,
			func(ctx context.Context, p *Transport) error { return p.PlayPause(ctx) }},
		{"next", `tell application "Spotify" to next track`,
			func(ctx context.Context, p *Transport) error { return p.Next(ctx) }},
		{"previous", `tell application "Spotify" to previous track`,
			func(ctx context.Context, p *Transport) error { return p.Previous(ctx) }},
		{"seek", `tell application "Spotify" to set player position to 12.500`,
			func(ctx context.Context, p *Transport) error { return p.Seek(ctx, 12500*time.Millisecond) }},
		{"volume", `tell application "Spotify" to set sound volume to 35`,
			func(ctx context.Context, p *Transport) error { return p.Volume(ctx, 35) }},
		{"shuffle on", `tell application "Spotify" to set shuffling to true`,
			func(ctx context.Context, p *Transport) error { return p.Shuffle(ctx, true) }},
		{"shuffle off", `tell application "Spotify" to set shuffling to false`,
			func(ctx context.Context, p *Transport) error { return p.Shuffle(ctx, false) }},
		{"repeat on", `tell application "Spotify" to set repeating to true`,
			func(ctx context.Context, p *Transport) error { return p.Repeat(ctx, true) }},
		{"repeat off", `tell application "Spotify" to set repeating to false`,
			func(ctx context.Context, p *Transport) error { return p.Repeat(ctx, false) }},
		{"play a URI", `tell application "Spotify" to play track "spotify:album:1"`,
			func(ctx context.Context, p *Transport) error { return p.Play(ctx, "spotify:album:1") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{}
			if err := test.call(context.Background(), New(runner)); err != nil {
				t.Fatalf("call: %v", err)
			}
			calls := runner.calls()
			if len(calls) != 1 {
				t.Fatalf("ran %d scripts, want 1", len(calls))
			}
			if want := guarded(test.body); calls[0] != want {
				t.Errorf("script =\n%s\nwant\n%s", calls[0], want)
			}
		})
	}
}

func TestCommandsRejectValuesOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		err  error
		call func(context.Context, *Transport) error
	}{
		{"seek before the start", ErrRange,
			func(ctx context.Context, p *Transport) error { return p.Seek(ctx, -time.Second) }},
		{"volume below zero", ErrRange,
			func(ctx context.Context, p *Transport) error { return p.Volume(ctx, -1) }},
		{"volume above one hundred", ErrRange,
			func(ctx context.Context, p *Transport) error { return p.Volume(ctx, 101) }},
		{"a string that is not a URI", ErrURI,
			func(ctx context.Context, p *Transport) error { return p.Play(ctx, "https://open.spotify.com/album/1") }},
		{"a URI without its kind", ErrURI,
			func(ctx context.Context, p *Transport) error { return p.Play(ctx, "spotify:") }},
		{"a URI with a quote", ErrURI,
			func(ctx context.Context, p *Transport) error {
				return p.Play(ctx, `spotify:album:1\" & (do shell script \"true\")`)
			}},
		{"a URI with a newline", ErrURI,
			func(ctx context.Context, p *Transport) error { return p.Play(ctx, "spotify:album:1\ntell application") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{}
			err := test.call(context.Background(), New(runner))
			if !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
			if calls := runner.calls(); len(calls) != 0 {
				t.Errorf("ran %d scripts for a rejected value, want 0", len(calls))
			}
		})
	}
}

func TestAutomationRefusalIsTyped(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		return "", &CommandError{
			Stderr: "execution error: Not authorized to send Apple events to Spotify. (-1743)",
			Err:    errors.New("exit status 1"),
		}
	}}

	_, err := New(runner).Read(context.Background())
	if !errors.Is(err, ErrAutomation) {
		t.Fatalf("Read error = %v, want ErrAutomation", err)
	}
	if errors.Is(err, ErrNotRunning) {
		t.Errorf("a refused Apple event is not a stopped Spotify: %v", err)
	}
	if !strings.Contains(err.Error(), "-1743") {
		t.Errorf("error %q does not keep osascript's reason", err)
	}
}

func TestNotRunningIsTyped(t *testing.T) {
	tests := map[string]*CommandError{
		"the guard's token": nil,
		"osascript's -600": {
			Stderr: "execution error: Spotify got an error: Application isn't running. (-600)",
			Err:    errors.New("exit status 1"),
		},
	}

	for name, commandErr := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{run: func(context.Context, string) (string, error) {
				if commandErr == nil {
					return notRunningToken + "\n", nil
				}
				return "", commandErr
			}}

			_, err := New(runner).Read(context.Background())
			if !errors.Is(err, ErrNotRunning) {
				t.Fatalf("Read error = %v, want ErrNotRunning", err)
			}
		})
	}
}

func TestReadTimesOut(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	player := New(runner)
	player.Timeout = 25 * time.Millisecond

	_, err := player.Read(context.Background())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Read error = %v, want ErrTimeout", err)
	}
}

func TestOneCallInFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runner := &fakeRunner{run: func(context.Context, string) (string, error) {
		once.Do(func() { close(started) })
		<-release
		return "", nil
	}}
	player := New(runner)

	firstDone := make(chan error, 1)
	go func() { firstDone <- player.Next(context.Background()) }()
	<-started

	if err := player.PlayPause(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("second call error = %v, want ErrBusy", err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first call: %v", err)
	}

	// The slot is free again once the first call returns.
	if err := player.PlayPause(context.Background()); err != nil {
		t.Fatalf("call after the first finished: %v", err)
	}
}

func TestSnapshotRateFractionAndAt(t *testing.T) {
	readAt := time.UnixMilli(1_700_000_000_123)
	playing := Snapshot{
		State:    StatePlaying,
		Duration: 4 * time.Second,
		Position: time.Second,
		ReadAt:   readAt,
	}

	if got := playing.Rate(); math.Abs(got-0.25) > 1e-9 {
		t.Errorf("Rate = %v, want 0.25", got)
	}
	if got := playing.Fraction(); math.Abs(got-0.25) > 1e-9 {
		t.Errorf("Fraction = %v, want 0.25", got)
	}
	if got := playing.At(); got != readAt.UnixMilli() {
		t.Errorf("At = %d, want %d", got, readAt.UnixMilli())
	}

	paused := playing
	paused.State = StatePaused
	if paused.Rate() != 0 {
		t.Errorf("paused Rate = %v, want 0", paused.Rate())
	}

	empty := Snapshot{State: StatePlaying}
	if empty.Rate() != 0 || empty.Fraction() != 0 {
		t.Errorf("empty Rate/Fraction = %v/%v, want 0/0", empty.Rate(), empty.Fraction())
	}

	over := playing
	over.Position = 5 * time.Second
	if got := over.Fraction(); got != 1 {
		t.Errorf("Fraction past the end = %v, want 1", got)
	}
	under := playing
	under.Position = -time.Second
	if got := under.Fraction(); got != 0 {
		t.Errorf("Fraction before the start = %v, want 0", got)
	}
}

func TestStateString(t *testing.T) {
	tests := map[State]string{
		StateStopped: "stopped",
		StatePlaying: "playing",
		StatePaused:  "paused",
	}
	for state, want := range tests {
		if got := state.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", state, got, want)
		}
	}
}
