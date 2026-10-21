package usage

import (
	"context"
	"sync"

	"kero/internal/resources"
	accountusage "kero/internal/usage"
)

// Source is the account usage and the machine sample a pane shows. The app
// reads them from internal/usage and internal/resources; a test gives its
// own, so that no test reads a real account, samples the real machine, or
// touches the network.
type Source interface {
	// Providers is every account the app knows how to read.
	Providers() []accountusage.Report

	// Installed is the accounts that look set up on this Mac. It is empty
	// when none can be told apart, and then every provider is shown.
	Installed() []string

	// Refresh reads the accounts, keeping the last success of a provider
	// that fails.
	Refresh(ctx context.Context, previous []accountusage.Report) []accountusage.Report

	// Sample reads the CPU and the memory of this Mac.
	Sample() (resources.Sample, error)

	// ResetSample forgets the last CPU reading, so that the next sample does
	// not average over the time no pane was on screen.
	ResetSample()
}

// liveSource is the app's own accounts and machine.
type liveSource struct {
	mu      sync.Mutex
	sampler *resources.Sampler
	err     error
}

func (s *liveSource) Providers() []accountusage.Report { return accountusage.Providers() }

func (s *liveSource) Installed() []string { return accountusage.Installed() }

func (s *liveSource) Refresh(ctx context.Context, previous []accountusage.Report) []accountusage.Report {
	return accountusage.Refresh(ctx, previous)
}

func (s *liveSource) Sample() (resources.Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sampler == nil && s.err == nil {
		s.sampler, s.err = resources.NewSampler()
	}
	if s.err != nil {
		return resources.Sample{}, s.err
	}
	return s.sampler.Read()
}

// ResetSample drops the sampler; the next sample makes a new one, which has
// no earlier reading to compare and so shows no CPU until the one after.
func (s *liveSource) ResetSample() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampler, s.err = nil, nil
}
