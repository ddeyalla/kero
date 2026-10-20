package daemon

import (
	"testing"
	"testing/synctest"
	"time"

	"kero/internal/proto"
)

// A TUI can finish its synchronized redraw just after the initial coalescing
// timer fires. New output must not sit behind the 50ms hold retry.
func TestSynchronizedRedrawReleasedPromptly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := testPane(t)
		mirror, err := s.term.NewMirror()
		if err != nil {
			t.Fatal(err)
		}
		defer mirror.Close()
		mirror.Frame()
		client := newAttachment(nil)
		client.mirror = mirror
		s.mu.Lock()
		s.clients[client] = struct{}{}
		s.mu.Unlock()
		s.output([]byte("\x1b[?2026h\r\x1b[Knew prompt"))
		time.Sleep(frameDelay)
		synctest.Wait()
		select {
		case <-client.out:
			t.Fatal("painted a partially synchronized redraw")
		default:
		}
		s.output([]byte("\x1b[?2026l"))
		time.Sleep(frameDelay)
		synctest.Wait()
		select {
		case frame := <-client.out:
			if frame[0] != proto.FrameOutput {
				t.Fatal("not an output frame")
			}
		default:
			t.Fatal("completed redraw is still waiting for the 50ms hold retry")
		}
	})
}
