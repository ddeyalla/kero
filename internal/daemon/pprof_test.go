package daemon

import (
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

// kerod serves profiles on the loopback alone: an address without a host,
// or of every interface, or of another machine's, is refused.
func TestProfilesLoopbackOnly(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:6060":    true,
		"127.0.0.2:6060":    true,
		"[::1]:6060":        true,
		"localhost:6060":    true,
		"LOCALHOST:0":       true,
		":6060":             false,
		"0.0.0.0:6060":      false,
		"[::]:6060":         false,
		"192.168.1.20:6060": false,
		"example.com:6060":  false,
		"127.0.0.1":         false,
		"127.0.0.1:":        false,
		"nonsense":          false,
	} {
		if err := loopbackAddr(addr); (err == nil) != ok {
			t.Errorf("loopbackAddr(%q) = %v, want ok %v", addr, err, ok)
		}
	}
}

// Unset, or refused, KERO_PPROF serves nothing and labels nothing.
func TestProfilesOff(t *testing.T) {
	lg := log.New(io.Discard, "", 0)
	for _, addr := range []string{"", ":6060", "0.0.0.0:0"} {
		stop := serveProfiles(addr, lg)
		if profiling.Load() {
			t.Errorf("KERO_PPROF=%q: profiling", addr)
		}
		stop()
	}
}

// Set, kerod serves the profiles while it runs, and stops as it ends.
func TestProfilesServe(t *testing.T) {
	var logged strings.Builder
	stop := serveProfiles("127.0.0.1:0", log.New(&logged, "", 0))
	defer stop()
	if !profiling.Load() {
		t.Fatalf("not profiling: %s", logged.String())
	}
	url := strings.TrimSpace(strings.TrimPrefix(logged.String(), "serving profiles on "))
	resp, err := http.Get(url + "goroutine?debug=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "goroutine profile") {
		t.Fatalf("%s: %d %.200s", url, resp.StatusCode, body)
	}
	ran := false
	profiled("pane.tick", func() { ran = true })
	if !ran {
		t.Fatal("profiled did not run its work")
	}
	stop()
	if profiling.Load() {
		t.Fatal("still profiling after stop")
	}
	if _, err := http.Get(url); err == nil {
		t.Fatal("still serving after stop")
	}
}

// While kerod does not profile, labelling its work allocates nothing: the
// frames sent to clients go through it, each one.
func TestProfiledFree(t *testing.T) {
	n := 0
	if a := testing.AllocsPerRun(100, func() {
		profiled("attachment.send", func() { n++ })
	}); a != 0 {
		t.Fatalf("profiled allocates %v times a call while not profiling", a)
	}
}
