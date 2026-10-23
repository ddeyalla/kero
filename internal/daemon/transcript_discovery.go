package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// codexTranscriptRoot follows Codex's own configuration directory, as the
// usage scanner and hook installer do.
func codexTranscriptRoot() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".codex", "sessions")
	}
	return ""
}

// transcriptDiscovery reads a Codex session tree in bounded slices. A
// missing transcript in a large archive must not monopolize a scan pass,
// nor make sessions beyond the first slice permanently undiscoverable.
// Only one directory stays open between passes; close releases it when
// the pane closes or the agent session changes.
type transcriptDiscovery struct {
	mu     sync.Mutex
	dirs   []string
	dir    *os.File
	path   string
	best   string
	bestAt time.Time
	closed bool
}

func newTranscriptDiscovery(root string) *transcriptDiscovery {
	return &transcriptDiscovery{dirs: []string{root}}
}

// next consumes at most budget directory entries or directory opens. A
// true pending means the next pass continues this same search. The result
// becomes final only at the end, so duplicate rollouts keep newest-wins.
func (s *transcriptDiscovery) next(session string, budget int) (path string, pending bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	suffix := "-" + session + ".jsonl"
	for budget > 0 && !s.closed {
		if s.dir == nil {
			if len(s.dirs) == 0 {
				return s.best, false
			}
			s.path, s.dirs = s.dirs[len(s.dirs)-1], s.dirs[:len(s.dirs)-1]
			s.dir, _ = os.Open(s.path)
			budget--
			if s.dir == nil {
				continue
			}
		}
		if budget == 0 {
			break
		}
		entries, err := s.dir.ReadDir(min(budget, 64))
		budget -= len(entries)
		for _, entry := range entries {
			path := filepath.Join(s.path, entry.Name())
			if entry.IsDir() {
				s.dirs = append(s.dirs, path)
				continue
			}
			// Do not follow a symlink to another tree or to a transcript.
			if !entry.Type().IsRegular() {
				continue
			}
			name := entry.Name()
			if name != session+".jsonl" && !(strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, suffix)) {
				continue
			}
			if info, err := entry.Info(); err == nil && (s.best == "" || info.ModTime().After(s.bestAt) || info.ModTime().Equal(s.bestAt) && path > s.best) {
				s.best, s.bestAt = path, info.ModTime()
			}
		}
		if err != nil || len(entries) == 0 {
			s.dir.Close()
			s.dir = nil
		}
	}
	if s.closed {
		return "", false
	}
	if s.dir != nil || len(s.dirs) > 0 {
		return "", true
	}
	return s.best, false
}

func (s *transcriptDiscovery) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.dirs = nil
	if s.dir != nil {
		s.dir.Close()
		s.dir = nil
	}
}
