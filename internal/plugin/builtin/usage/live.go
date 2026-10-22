package usage

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"kero/internal/proto"
	accountusage "kero/internal/usage"
	"kero/internal/usage/cost"
)

// Source is the account usage and the cost a pane shows. The app reads them
// from internal/usage and its cost package; a test gives its own, so that no
// test reads a real account, scans real logs, or touches the network.
type Source interface {
	// Providers is every account the app knows how to read.
	Providers() []accountusage.Report

	// Installed is the accounts that look set up on this Mac. It is empty
	// when none can be told apart, and then every provider is shown.
	Installed() []string

	// Refresh reads the accounts, keeping the last success of a provider
	// that fails.
	Refresh(ctx context.Context, previous []accountusage.Report) []accountusage.Report

	// RefreshCost reads what the agents' logs gained since the last read. The
	// first read is slow, so it runs off the render path and ends when ctx
	// does.
	RefreshCost(ctx context.Context, now time.Time) error

	// Spends is the cost of every range as of the last RefreshCost, the
	// costliest provider first. It reads memory only.
	Spends(now time.Time) map[cost.Range][]cost.Spend

	// Flush writes what the cost reader learned, if it changed. The plugin
	// calls it once, when it closes.
	Flush() error
}

// costFolder is where the plugin keeps what it learned from the logs, under
// the plugin's data folder in the app's directory. The directory follows
// $KERO_HOME, so a test app does not share the daily app's cache.
func costFolder() string { return filepath.Join(proto.Dir(), "plugin-data", "usage") }

// liveSource is the app's own accounts and logs.
type liveSource struct {
	once   sync.Once
	reader *cost.Reader
}

func (s *liveSource) Providers() []accountusage.Report { return accountusage.Providers() }

func (s *liveSource) Installed() []string { return accountusage.Installed() }

func (s *liveSource) Refresh(ctx context.Context, previous []accountusage.Report) []accountusage.Report {
	return accountusage.Refresh(ctx, previous)
}

// costs is the reader of the logs, made once: it loads the cache of the
// earlier runs.
func (s *liveSource) costs() *cost.Reader {
	s.once.Do(func() { s.reader = cost.NewReader(costFolder()) })
	return s.reader
}

func (s *liveSource) RefreshCost(ctx context.Context, now time.Time) error {
	return s.costs().Refresh(ctx, now)
}

func (s *liveSource) Spends(now time.Time) map[cost.Range][]cost.Spend {
	return s.costs().Spends(now)
}

// Flush saves the reader's cache when a read made one.
func (s *liveSource) Flush() error {
	if s.reader == nil {
		return nil
	}
	return s.reader.Flush()
}
