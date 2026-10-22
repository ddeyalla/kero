package settings

import "testing"

// None is -1, not 0: a terminal of 0 takes MyGo's default, 10 MB.
func TestScrollbackBytes(t *testing.T) {
	for mb, want := range map[int]int{0: -1, 1: 1 << 20, 4: 4 << 20} {
		if got := (Settings{Scrollback: mb}).ScrollbackBytes(); got != want {
			t.Errorf("%d MB: %d, want %d", mb, got, want)
		}
	}
}
