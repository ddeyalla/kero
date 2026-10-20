// Package resources samples machine-wide CPU and physical memory off the UI
// thread. CPU is the busy share across all cores, between two observations.
package resources

import "time"

type Sample struct {
	CPU         *float64  `json:"cpuPercent,omitempty"`
	MemoryUsed  uint64    `json:"memoryUsedBytes"`
	MemoryTotal uint64    `json:"memoryTotalBytes"`
	At          time.Time `json:"sampledAt"`
}

type reading struct {
	ticks       [4]uint32
	used, total uint64
}

type Sampler struct {
	read     func() (reading, error)
	previous *[4]uint32
}

func NewSampler() (*Sampler, error) {
	read, err := nativeReader()
	if err != nil {
		return nil, err
	}
	return &Sampler{read: read}, nil
}

func (s *Sampler) Read() (Sample, error) {
	r, err := s.read()
	if err != nil {
		return Sample{}, err
	}
	out := Sample{MemoryUsed: min(r.used, r.total), MemoryTotal: r.total, At: time.Now()}
	if s.previous != nil {
		if n, ok := cpuPercent(*s.previous, r.ticks); ok {
			out.CPU = &n
		}
	}
	s.previous = &r.ticks
	return out, nil
}

func cpuPercent(before, after [4]uint32) (float64, bool) {
	var total, idle uint64
	for i := range after {
		delta := uint64(after[i] - before[i])
		total += delta
		if i == 2 {
			idle = delta
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(total-idle) / float64(total) * 100, true
}
