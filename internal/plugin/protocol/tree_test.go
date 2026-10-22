package protocol

import "testing"

func TestFormatClock(t *testing.T) {
	for in, want := range map[float64]string{-3: "0:00", 0: "0:00", 5.9: "0:05", 65: "1:05", 600: "10:00", 3725: "1:02:05"} {
		if got := FormatClock(in); got != want {
			t.Errorf("FormatClock(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAdvanceValue(t *testing.T) {
	if got := AdvanceValue(10, 1, 1000, 3500, 0, 100); got != 12.5 {
		t.Fatalf("advanced = %v, want 12.5", got)
	}
	if got := AdvanceValue(10, 1, 1000, 999000, 0, 100); got != 100 {
		t.Fatalf("clamped = %v, want 100", got)
	}
	if got := AdvanceValue(10, 0, 1000, 5000, 0, 100); got != 10 {
		t.Fatalf("no rate = %v, want 10", got)
	}
	if got := AdvanceValue(10, 1, 0, 5000, 0, 100); got != 10 {
		t.Fatalf("no instant = %v, want 10", got)
	}
	if got := AdvanceValue(10, 1, 5000, 1000, 0, 100); got != 6 {
		t.Fatalf("now before at = %v, want 6", got)
	}
}
