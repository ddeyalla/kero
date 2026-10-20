package resources

import (
	"testing"
	"time"
)

func TestCPUInterval(t *testing.T) {
	previous := [4]uint32{100, 100, 800, 0}
	current := [4]uint32{120, 110, 870, 0}
	if got, ok := cpuPercent(previous, current); !ok || got != 30 {
		t.Fatalf("CPU %v, valid %v", got, ok)
	}
	if _, ok := cpuPercent(current, current); ok {
		t.Fatal("zero interval claimed a CPU reading")
	}
	previous = [4]uint32{^uint32(0) - 4, 10, 100, 0}
	current = [4]uint32{5, 10, 110, 0}
	if got, ok := cpuPercent(previous, current); !ok || got != 50 {
		t.Fatalf("counter wrap: %v, %v", got, ok)
	}
}

func TestReadLocalResources(t *testing.T) {
	s, err := NewSampler()
	if err != nil {
		t.Skip(err)
	}
	first, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if first.CPU != nil {
		t.Fatal("first observation claims an interval")
	}
	if first.MemoryTotal == 0 || first.MemoryUsed > first.MemoryTotal {
		t.Fatalf("invalid memory: %+v", first)
	}
	time.Sleep(50 * time.Millisecond)
	next, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if next.CPU == nil || *next.CPU < 0 || *next.CPU > 100 {
		t.Fatalf("invalid CPU: %+v", next)
	}
}
