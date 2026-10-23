package spotify

import (
	"context"
	"testing"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/protocol"
)

type delayedSnapshotPlayer struct {
	transport.Player
	started chan struct{}
	release chan struct{}
}

func (p *delayedSnapshotPlayer) Read(ctx context.Context) (transport.Snapshot, error) {
	close(p.started)
	select {
	case <-p.release:
		return transport.Snapshot{State: transport.StatePlaying, Track: "Old track"}, nil
	case <-ctx.Done():
		return transport.Snapshot{}, ctx.Err()
	}
}

func TestReadDoesNotOverwriteNewerPlaybackPush(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	player := &delayedSnapshotPlayer{Player: p.player, started: make(chan struct{}), release: make(chan struct{})}
	p.player = player
	done := make(chan struct{})
	go func() { p.readNow(); close(done) }()
	<-player.started
	p.playback(transport.Snapshot{State: transport.StatePlaying, Track: "New track"})
	close(player.release)
	<-done
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "New track" {
		t.Fatalf("older read overwrote pushed state: %q", s.snap.Track)
	}
}
