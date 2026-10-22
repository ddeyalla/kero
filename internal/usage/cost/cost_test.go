package cost

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"
)

var testLoc = time.FixedZone("test", 5*3600+1800)

// testNow is noon on 2026-10-22 in testLoc.
var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, testLoc)

// env points every source at an empty temp tree and returns the roots.
func env(t *testing.T) (claude, codex string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	claude = filepath.Join(home, "claude")
	codex = filepath.Join(home, "codex")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	return claude, codex
}

func fixture(t *testing.T, name, dst string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	write(t, dst, data)
}

func write(t *testing.T, dst string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dst, testNow, testNow); err != nil {
		t.Fatal(err)
	}
}

func newReader(t *testing.T, dir string) *Reader {
	r := NewReader(dir)
	r.loc = testLoc
	return r
}

func refresh(t *testing.T, r *Reader) {
	t.Helper()
	if err := r.Refresh(context.Background(), testNow); err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, got []Spend, provider string) Spend {
	t.Helper()
	for _, s := range got {
		if s.Provider == provider {
			return s
		}
	}
	t.Fatalf("no spend for %q in %+v", provider, got)
	return Spend{}
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.9f, want %.9f", what, got, want)
	}
}

func TestClaude(t *testing.T) {
	claude, _ := env(t)
	fixture(t, "claude-a.jsonl", filepath.Join(claude, "projects", "p", "a.jsonl"))
	fixture(t, "claude-b.jsonl", filepath.Join(claude, "projects", "p", "b.jsonl"))
	r := newReader(t, "")
	refresh(t, r)

	// msg_a counts once, at its largest output (80), across both files:
	// opus 5.5 = 100*4 + 80*20 + 1000*8 (1h write) + 2000*0.2 per million.
	// msg_c (after local midnight) is 1M sonnet input = $2. Mystery adds 20 tokens.
	tests := []struct {
		name   string
		rg     Range
		usd    float64
		tokens int64
	}{
		{"today", Today, 0.0104 + 2, 3180 + 1_000_000 + 20},
		{"yesterday", Yesterday, 0.012, 2000}, // msg_b: 1000*2 + 1000*10, before local midnight
		{"30 days", Last30Days, 0.0104 + 2 + 0.012, 3180 + 1_000_000 + 20 + 2000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := find(t, r.Spend(tt.rg, testNow), "claude")
			near(t, "usd", s.USD, tt.usd)
			if s.Tokens != tt.tokens {
				t.Errorf("tokens = %d, want %d", s.Tokens, tt.tokens)
			}
		})
	}
	if s := find(t, r.Spend(Today, testNow), "claude"); !slices.Equal(s.Unknown, []string{"mystery-1"}) {
		t.Errorf("unknown = %v, want [mystery-1]", s.Unknown)
	}
}

func TestCodexDeltas(t *testing.T) {
	_, codex := env(t)
	fixture(t, "codex.jsonl", filepath.Join(codex, "sessions", "2026", "10", "22", "rollout-a.jsonl"))
	r := newReader(t, "")
	refresh(t, r)

	// Deltas: (400 in, 600 cached, 100 out), (600, 1400, 200), then a reset to
	// (500, 0, 50). The repeated total and the null info add nothing.
	s := find(t, r.Spend(Today, testNow), "codex")
	if s.Tokens != 1500+2000+350 {
		t.Errorf("tokens = %d, want 3850", s.Tokens)
	}
	near(t, "usd", s.USD, (1500*1.25+350*10+2000*0.125)/1e6)
	if !slices.Equal(s.Estimated, []string{"gpt-6.1-sol"}) {
		t.Errorf("estimated = %v", s.Estimated)
	}
}

func TestIncrementalAppend(t *testing.T) {
	_, codex := env(t)
	path := filepath.Join(codex, "sessions", "2026", "10", "22", "rollout-a.jsonl")
	data, _ := os.ReadFile(filepath.Join("testdata", "codex.jsonl"))
	lines := splitLines(data)
	head := joinLines(lines[:5])
	write(t, path, head)

	r := newReader(t, "")
	refresh(t, r)
	if r.bytesRead != int64(len(head)) {
		t.Fatalf("cold read %d bytes, want %d", r.bytesRead, len(head))
	}
	before := find(t, r.Spend(Today, testNow), "codex").Tokens

	// Append the rest, ending with a partial line that must wait.
	rest := joinLines(lines[5:])
	partial := rest[:len(rest)-10]
	write(t, path, append(slices.Clone(head), partial...))
	refresh(t, r)
	wantRead := int64(len(joinLines(lines[5:6]))) // only the complete null-info line
	if r.bytesRead != wantRead {
		t.Errorf("read %d bytes, want %d", r.bytesRead, wantRead)
	}

	write(t, path, append(slices.Clone(head), rest...))
	refresh(t, r)
	if r.bytesRead != int64(len(rest))-wantRead {
		t.Errorf("read %d bytes, want %d", r.bytesRead, int64(len(rest))-wantRead)
	}
	after := find(t, r.Spend(Today, testNow), "codex").Tokens
	if before != 1000+100+(3000-1000)+(300-100) || after != 3850 {
		t.Errorf("tokens before %d after %d", before, after)
	}

	refresh(t, r)
	if r.bytesRead != 0 {
		t.Errorf("unchanged refresh read %d bytes", r.bytesRead)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	claude, codex := env(t)
	fixture(t, "claude-a.jsonl", filepath.Join(claude, "projects", "p", "a.jsonl"))
	fixture(t, "codex.jsonl", filepath.Join(codex, "sessions", "2026", "10", "22", "rollout-a.jsonl"))
	dir := t.TempDir()
	r := newReader(t, dir)
	refresh(t, r)
	want := r.Spend(Last30Days, testNow)

	r2 := newReader(t, dir)
	got := r2.Spend(Last30Days, testNow)
	if len(got) != len(want) || len(got) != 2 {
		t.Fatalf("restored %+v, want %+v", got, want)
	}
	for i := range got {
		near(t, got[i].Provider, got[i].USD, want[i].USD)
		if got[i].Tokens != want[i].Tokens {
			t.Errorf("%s tokens = %d, want %d", got[i].Provider, got[i].Tokens, want[i].Tokens)
		}
	}
	refresh(t, r2)
	if r2.bytesRead != 0 {
		t.Errorf("restart reparsed %d bytes", r2.bytesRead)
	}
	// The restored duplicate index still blocks a repeated message.
	fixture(t, "claude-b.jsonl", filepath.Join(claude, "projects", "p", "b.jsonl"))
	refresh(t, r2)
	if s := find(t, r2.Spend(Today, testNow), "claude"); s.Tokens != 3180+1_000_000+20 {
		t.Errorf("tokens after dup file = %d", s.Tokens)
	}
}

func TestCorruptCacheIgnored(t *testing.T) {
	env(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cacheFile), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newReader(t, dir)
	refresh(t, r)
	if got := r.Spend(Today, testNow); len(got) != 0 {
		t.Errorf("spend = %+v", got)
	}
}

func TestOldFilesAndDaysDropped(t *testing.T) {
	claude, _ := env(t)
	p := filepath.Join(claude, "projects", "p", "a.jsonl")
	fixture(t, "claude-a.jsonl", p)
	old := testNow.AddDate(0, 0, -40)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	r := newReader(t, "")
	refresh(t, r)
	if got := r.Spend(Last30Days, testNow); len(got) != 0 {
		t.Errorf("old file counted: %+v", got)
	}
}

func TestLongLineSkipped(t *testing.T) {
	claude, _ := env(t)
	data, _ := os.ReadFile(filepath.Join("testdata", "claude-b.jsonl"))
	big := make([]byte, 600<<10)
	for i := range big {
		big[i] = 'x'
	}
	line := append([]byte(`{"type":"assistant","usage":"`), big...)
	line = append(line, []byte("\"}\n")...)
	write(t, filepath.Join(claude, "projects", "p", "a.jsonl"), append(line, data...))
	r := newReader(t, "")
	refresh(t, r)
	if s := find(t, r.Spend(Today, testNow), "claude"); s.Tokens != 3180 {
		t.Errorf("tokens = %d, want 3180", s.Tokens)
	}
}

func TestOpenCode(t *testing.T) {
	tool, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("no sqlite3")
	}
	env(t)
	db := openCodeDB()
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	ms := testNow.UnixMilli()
	sql := `create table message(id text primary key, session_id text, time_created integer, time_updated integer, data text);
insert into message values('a','s',` + itoa(ms) + `,` + itoa(ms) + `,'{"role":"assistant","providerID":"opencode-go","modelID":"glm","cost":0.5,"tokens":{"input":100,"output":20,"reasoning":30,"cache":{"read":1000,"write":5}}}');
insert into message values('b','s',` + itoa(ms) + `,` + itoa(ms) + `,'{"role":"user"}');`
	if out, err := exec.Command(tool, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v %s", err, out)
	}
	r := newReader(t, "")
	refresh(t, r)
	s := find(t, r.Spend(Today, testNow), "opencode")
	near(t, "usd", s.USD, 0.5)
	if s.Tokens != 100+50+1000+5 || len(s.Unknown) != 0 {
		t.Errorf("spend = %+v", s)
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		model string
		in    float64
		q     Quality
	}{
		{"claude-opus-5-5-20261001", 4, Listed},
		{"claude-opus-4-8", 5, Listed},
		{"claude-opus-4-1-20250805", 15, Listed},
		{"anthropic/claude-opus-4.6", 5, Listed},
		{"claude-fable-5-1", 10, Listed},
		{"gpt-6.1-sol", 1.25, Estimated},
		{"gpt-6-luna", 0.25, Estimated},
		{"mystery-1", 0, Unknown},
	}
	for _, tt := range tests {
		p, q := Lookup(tt.model)
		if p.In != tt.in || q != tt.q {
			t.Errorf("Lookup(%q) = %v, %v; want in %v, %v", tt.model, p.In, q, tt.in, tt.q)
		}
	}
	if got := providerOf("gpt-6.1-sol", "claude"); got != "codex" {
		t.Errorf("providerOf = %q", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func splitLines(data []byte) [][]byte {
	var out [][]byte
	for len(data) > 0 {
		i := 0
		for i < len(data) && data[i] != '\n' {
			i++
		}
		out = append(out, data[:i+1])
		data = data[i+1:]
	}
	return out
}

func joinLines(lines [][]byte) []byte { return slices.Concat(lines...) }

// TestLive reads the real logs of this machine. It logs totals only.
func TestLive(t *testing.T) {
	if os.Getenv("SHIBU_COST_LIVE") != "1" {
		t.Skip("set SHIBU_COST_LIVE=1")
	}
	dir := t.TempDir()
	r := NewReader(dir)
	now := time.Now()
	start := time.Now()
	if err := r.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	cold, coldBytes := time.Since(start), r.bytesRead
	start = time.Now()
	_ = r.Refresh(context.Background(), now)
	t.Logf("cold %v (%d MB parsed), warm %v (%d bytes)", cold, coldBytes>>20, time.Since(start), r.bytesRead)
	for _, rg := range []struct {
		name string
		rg   Range
	}{{"today", Today}, {"yesterday", Yesterday}, {"30d", Last30Days}} {
		for _, s := range r.Spend(rg.rg, now) {
			t.Logf("%-9s %-9s $%.2f %.1fM tokens unknown=%v est=%v", rg.name, s.Provider, s.USD, float64(s.Tokens)/1e6, s.Unknown, s.Estimated)
		}
	}
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	t.Logf("heap after GC %d KB", ms.HeapAlloc>>10)
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(dir); true {
		for _, e := range ents {
			if info, err := e.Info(); err == nil {
				t.Logf("cache file %s: %d bytes", e.Name(), info.Size())
			}
		}
	}
	runtime.KeepAlive(r)
}

func BenchmarkWarmWalk(b *testing.B) {
	if os.Getenv("SHIBU_COST_LIVE") != "1" {
		b.Skip("set SHIBU_COST_LIVE=1")
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	for range b.N {
		n := 0
		for _, root := range claudeRoots() {
			n += len(listFiles(root, kindClaude, cutoff))
		}
		n += len(listFiles(codexRoot(), kindCodex, cutoff))
	}
}

func TestCancelResumes(t *testing.T) {
	_, codex := env(t)
	fixture(t, "codex.jsonl", filepath.Join(codex, "sessions", "2026", "10", "22", "rollout-a.jsonl"))
	r := newReader(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Refresh(ctx, testNow); err == nil {
		t.Fatal("canceled refresh returned nil")
	}
	refresh(t, r)
	got := r.Spends(testNow)
	if s := find(t, got[Today], "codex"); s.Tokens != 3850 {
		t.Errorf("tokens = %d, want 3850", s.Tokens)
	}
	if len(got) != 3 {
		t.Errorf("ranges = %d", len(got))
	}
}

func TestOpenCodeZeroCostNotListPriced(t *testing.T) {
	tool, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("no sqlite3")
	}
	env(t)
	db := openCodeDB()
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	ms := testNow.UnixMilli()
	sql := `create table message(id text primary key, session_id text, time_created integer, time_updated integer, data text);
insert into message values('a','s',` + itoa(ms) + `,` + itoa(ms) + `,'{"role":"assistant","providerID":"anthropic","modelID":"claude-opus-4-8","cost":0,"tokens":{"input":1000000,"output":1000000,"cache":{"read":0,"write":0}}}');`
	if out, err := exec.Command(tool, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v %s", err, out)
	}
	r := newReader(t, "")
	refresh(t, r)
	s := find(t, r.Spend(Today, testNow), "opencode")
	near(t, "usd", s.USD, 0)
	if s.Tokens != 2_000_000 || len(s.Unknown) != 0 || len(s.Estimated) != 0 {
		t.Errorf("spend = %+v", s)
	}
}

func TestCodexTruncationNotDoubleCounted(t *testing.T) {
	_, codex := env(t)
	path := filepath.Join(codex, "sessions", "2026", "10", "22", "rollout-a.jsonl")
	data, _ := os.ReadFile(filepath.Join("testdata", "codex.jsonl"))
	write(t, path, data)
	dir := t.TempDir()
	r := newReader(t, dir)
	refresh(t, r)
	if s := find(t, r.Spend(Today, testNow), "codex"); s.Tokens != 3850 {
		t.Fatalf("tokens = %d, want 3850", s.Tokens)
	}
	// The file is rewritten shorter, then the first part again from zero.
	lines := splitLines(data)
	write(t, path, joinLines(lines[:5]))
	if err := os.Chtimes(path, testNow.Add(time.Minute), testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	r = newReader(t, dir) // the contribution survives a restart
	refresh(t, r)
	if s := find(t, r.Spend(Today, testNow), "codex"); s.Tokens != 3300 {
		t.Errorf("tokens after truncate = %d", s.Tokens)
	}
}

func TestSaveThrottledAndFlush(t *testing.T) {
	claude, _ := env(t)
	dir := t.TempDir()
	r := newReader(t, dir)
	r.saveEvery = time.Hour
	fixture(t, "claude-a.jsonl", filepath.Join(claude, "projects", "p", "a.jsonl"))
	refresh(t, r) // first write is due at once
	path := filepath.Join(dir, cacheFile)
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info1.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info1.Mode().Perm())
	}
	fixture(t, "claude-b.jsonl", filepath.Join(claude, "projects", "p", "b.jsonl"))
	refresh(t, r)
	info2, _ := os.Stat(path)
	if info2.Size() != info1.Size() || !info2.ModTime().Equal(info1.ModTime()) {
		t.Error("cache rewritten inside the interval")
	}
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	info3, _ := os.Stat(path)
	if info3.Size() == info1.Size() {
		t.Error("flush did not write")
	}
}

func TestOldFormatCacheDiscarded(t *testing.T) {
	claude, _ := env(t)
	fixture(t, "claude-a.jsonl", filepath.Join(claude, "projects", "p", "a.jsonl"))
	dir := t.TempDir()
	r := newReader(t, dir)
	refresh(t, r)
	want := find(t, r.Spend(Last30Days, testNow), "claude").Tokens
	data, _ := os.ReadFile(filepath.Join(dir, cacheFile))
	for _, bad := range [][]byte{[]byte(`{"v":1,"files":{}}`), data[:len(data)/2], append(slices.Clone(data[:len(data)-3]), 0xff, 0xff, 0xff)} {
		d2 := t.TempDir()
		os.WriteFile(filepath.Join(d2, cacheFile), bad, 0o600)
		r2 := newReader(t, d2)
		refresh(t, r2)
		if got := find(t, r2.Spend(Last30Days, testNow), "claude").Tokens; got != want {
			t.Errorf("rescan tokens = %d, want %d", got, want)
		}
	}
}
