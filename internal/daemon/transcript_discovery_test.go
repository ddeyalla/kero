package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A large directory is read in slices, without losing files after the
// first budget, and the newest matching rollout wins across slices.
func TestCodexDiscoverySlices(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		scanBurn(t, filepath.Join(root, fmt.Sprintf("rollout-%02d-other.jsonl", i)), "{}\n")
	}
	older := filepath.Join(root, "2026", "01", "rollout-first-session.jsonl")
	newer := filepath.Join(root, "2026", "02", "rollout-second-session.jsonl")
	scanBurn(t, older, "{}\n")
	scanBurn(t, newer, "{}\n")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}
	search := newTranscriptDiscovery(root)
	defer search.close()
	if path, pending := search.next("session", 3); path != "" || !pending {
		t.Fatalf("first slice exhausted a large tree: path=%q pending=%v", path, pending)
	}
	for i := 0; i < 30; i++ {
		path, pending := search.next("session", 3)
		if !pending {
			if path != newer {
				t.Fatalf("found %q, want newest %q", path, newer)
			}
			return
		}
	}
	t.Fatal("discovery never completed")
}

// Symlinks cannot lead a discovery outside the agent's session directory.
func TestCodexDiscoveryIgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	scanBurn(t, filepath.Join(outside, "rollout-hidden-session.jsonl"), "{}\n")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	search := newTranscriptDiscovery(root)
	defer search.close()
	if path, pending := search.next("session", 256); path != "" || pending {
		t.Fatalf("followed symlink: %q %v", path, pending)
	}
}

// Closing a pane releases an unfinished traversal's directory descriptor.
func TestCodexDiscoveryClose(t *testing.T) {
	root := t.TempDir()
	scanBurn(t, filepath.Join(root, "rollout-a-other.jsonl"), "{}\n")
	search := newTranscriptDiscovery(root)
	if _, pending := search.next("missing", 1); !pending {
		t.Fatal("expected open directory")
	}
	dir := search.dir
	p := testPane(t)
	p.discovery = search
	p.close()
	if _, err := dir.ReadDir(1); err == nil {
		t.Fatal("closed search kept its directory open")
	}
	if path, pending := search.next("missing", 256); path != "" || pending {
		t.Fatalf("closed search continued: %q %v", path, pending)
	}
}
