//go:build darwin

package transport

import (
	"testing"
	"time"
)

func TestRunLoopPauseKeepsTheLoopFromSpinning(t *testing.T) {
	tests := []struct {
		name    string
		ran     bool
		elapsed time.Duration
		want    time.Duration
	}{
		{"no source rests the rest of a turn", false, 0, runLoopTurn},
		{"no source after part of a turn", false, 300 * time.Millisecond, runLoopTurn - 300*time.Millisecond},
		{"a turn that returned at once rests briefly", true, 0, runLoopRest},
		{"a turn that waited goes on", true, 200 * time.Millisecond, 0},
		{"a whole turn goes on", true, runLoopTurn, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runLoopPause(test.ran, test.elapsed); got != test.want {
				t.Errorf("runLoopPause(%v, %v) = %v, want %v", test.ran, test.elapsed, got, test.want)
			}
		})
	}
}
