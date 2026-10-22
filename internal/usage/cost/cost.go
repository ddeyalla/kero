// Package cost estimates what coding agents cost, from their local logs.
// It reads token counts, model names, timestamps and ids, and never message
// text. A refresh parses only the bytes appended since the last one.
package cost

import (
	"context"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// keepDays is how many days before today the reader keeps. It is one more than
// the longest range, so a day does not drop out while it is still shown.
const keepDays = 31

// maxWorkers bounds how many files parse at once.
const maxWorkers = 3

// Range is a window of local days.
type Range int

const (
	Today Range = iota
	Yesterday
	Last30Days
)

// Spend is the estimated cost of one provider over a range.
type Spend struct {
	Provider string
	USD      float64
	Tokens   int64
	// Unknown lists models without a price. Their tokens count, their dollars
	// do not.
	Unknown []string
	// Estimated lists models priced by a proxy, so USD is only an estimate.
	Estimated []string
}

// Bucket is the token and cost total of one provider and model on one day.
type Bucket struct {
	In  int64 `json:"i,omitempty"` // uncached input
	Out int64 `json:"o,omitempty"`
	CW5 int64 `json:"w,omitempty"`
	CW1 int64 `json:"h,omitempty"`
	CR  int64 `json:"r,omitempty"`
	// Direct is a cost that the tool recorded itself. When set, the tokens are
	// not priced again.
	Direct float64 `json:"d,omitempty"`
}

func (b *Bucket) add(o Bucket, sign int64) {
	b.In += sign * o.In
	b.Out += sign * o.Out
	b.CW5 += sign * o.CW5
	b.CW1 += sign * o.CW1
	b.CR += sign * o.CR
	b.Direct += float64(sign) * o.Direct
}

func (b Bucket) tokens() int64 { return b.In + b.Out + b.CW5 + b.CW1 + b.CR }

// usd prices a bucket. OpenCode always uses its own recorded cost, even when
// that is zero, because it runs free and subscription models that the price
// table would bill at list price.
func (b Bucket) usd(provider, model string) (float64, Quality) {
	if provider == "opencode" || b.Direct != 0 {
		return b.Direct, Listed
	}
	p, q := Lookup(model)
	usd := float64(b.In)*p.In + float64(b.Out)*p.Out + float64(b.CW5)*p.CW5 +
		float64(b.CW1)*p.CW1 + float64(b.CR)*p.CR
	return usd / 1e6, q
}

// event is one parsed usage record, before it joins the totals.
type event struct {
	day      string
	provider string
	model    string
	b        Bucket
	// key identifies a message that a log may repeat. Zero means no dedupe.
	key uint64
}

// cursor is how far a file has been read.
type cursor struct {
	Size  int64    `json:"s"`
	MTime int64    `json:"m"`
	Off   int64    `json:"o"`
	Model string   `json:"l,omitempty"` // Codex: the model of the current turn
	Prev  [3]int64 `json:"p,omitempty"` // Codex: previous total input, cached, output
	// Contrib is what the file added to the kept days, keyed "day|provider|model".
	// Codex only: a file that shrinks has its old share taken out before it is
	// read again. A stored cursor is never changed, only replaced.
	Contrib map[string]Bucket `json:"c,omitempty"`
}

// seenEntry is a repeated message that was already counted, kept small because
// there is one per message. The model and provider are an index into
// Reader.pms, and the day counts days since 1970 in UTC.
type seenEntry struct {
	Day                   uint16
	PM                    uint16
	In, Out, CW5, CW1, CR uint32
}

func (e seenEntry) bucket() Bucket {
	return Bucket{In: int64(e.In), Out: int64(e.Out), CW5: int64(e.CW5), CW1: int64(e.CW1), CR: int64(e.CR)}
}

func (e seenEntry) tokens() int64 { return e.bucket().tokens() }

type state struct {
	Version int
	Files   map[string]*cursor
	Days    map[string]map[string]Bucket // day -> "provider|model" -> total
	DB      int64                        // OpenCode: stamp of the last read
}

const stateVersion = 2

// saveEvery is how often the cache is written when it changed.
const saveEvery = 30 * time.Minute

// dayNum turns a "2006-01-02" day into days since 1970. A bad day gives 0.
func dayNum(day string) uint16 {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return 0
	}
	return uint16(t.Unix() / 86400)
}

func dayStr(n uint16) string { return time.Unix(int64(n)*86400, 0).UTC().Format("2006-01-02") }

// fitsSeen reports whether a bucket fits the compact entry.
func fitsSeen(b Bucket) bool {
	for _, v := range [...]int64{b.In, b.Out, b.CW5, b.CW1, b.CR} {
		if v < 0 || v > math.MaxUint32 {
			return false
		}
	}
	return true
}

// Reader keeps the incremental totals.
type Reader struct {
	dir string
	loc *time.Location

	refreshMu sync.Mutex // one refresh at a time
	saveMu    sync.Mutex // one cache write at a time
	mu        sync.Mutex // guards the fields below
	st        state
	seenIdx   map[uint64]seenEntry
	pms       []string          // interned "provider|model" keys
	pmIdx     map[string]uint16 // index of each key in pms
	dirty     bool
	lastSave  time.Time
	saveEvery time.Duration

	bytesRead int64 // bytes parsed by the last refresh, for tests and tuning
}

// NewReader returns a reader that persists its cache under stateDir. An empty
// stateDir keeps the cache in memory only.
func NewReader(stateDir string) *Reader {
	r := &Reader{dir: stateDir, loc: time.Local, saveEvery: saveEvery}
	r.reset()
	r.load()
	return r
}

func (r *Reader) reset() {
	r.st = state{Version: stateVersion, Files: map[string]*cursor{}, Days: map[string]map[string]Bucket{}}
	r.seenIdx = map[uint64]seenEntry{}
	r.pms, r.pmIdx = nil, map[string]uint16{}
}

// intern returns the index of a "provider|model" key. The caller holds r.mu.
func (r *Reader) intern(pm string) (uint16, bool) {
	if i, ok := r.pmIdx[pm]; ok {
		return i, true
	}
	if len(r.pms) >= math.MaxUint16 {
		return 0, false
	}
	i := uint16(len(r.pms))
	r.pms = append(r.pms, pm)
	r.pmIdx[pm] = i
	return i, true
}

func dayKey(t time.Time, loc *time.Location) string { return t.In(loc).Format("2006-01-02") }

// Refresh reads what the logs gained since the last refresh. A bad line or
// file is skipped. It returns an error only when the context ends.
func (r *Reader) Refresh(ctx context.Context, now time.Time) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.bytesRead = 0

	y, m, d := now.In(r.loc).Date()
	cutoff := time.Date(y, m, d, 0, 0, 0, 0, r.loc).AddDate(0, 0, -keepDays)
	cutoffDay := dayKey(cutoff, r.loc)

	var jobs []job
	for _, root := range claudeRoots() {
		jobs = append(jobs, listFiles(root, kindClaude, cutoff)...)
	}
	if root := codexRoot(); root != "" {
		jobs = append(jobs, listFiles(root, kindCodex, cutoff)...)
	}

	live := map[string]bool{}
	for _, j := range jobs {
		live[j.path] = true
	}
	// Files parse in parallel, a few at a time to keep the CPU load low.
	work := make(chan job)
	var wg sync.WaitGroup
	for range min(maxWorkers, runtime.NumCPU()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				r.readFile(ctx, j, cutoffDay)
			}
		}()
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		work <- j
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.readOpenCode(ctx, now, cutoff, cutoffDay)

	r.mu.Lock()
	for p := range r.st.Files {
		if !live[p] {
			delete(r.st.Files, p)
			r.dirty = true
		}
	}
	r.prune(cutoffDay)
	due := r.dirty && time.Since(r.lastSave) >= r.saveEvery
	r.mu.Unlock()
	if due {
		_ = r.save()
	}
	return ctx.Err()
}

// prune drops days older than the cutoff. The caller holds r.mu.
func (r *Reader) prune(cutoffDay string) {
	for day := range r.st.Days {
		if day < cutoffDay {
			delete(r.st.Days, day)
			r.dirty = true
		}
	}
	cut := dayNum(cutoffDay)
	for k, s := range r.seenIdx {
		if s.Day < cut {
			delete(r.seenIdx, k)
			r.dirty = true
		}
	}
}

type kind int

const (
	kindClaude kind = iota
	kindCodex
)

type job struct {
	path  string
	kind  kind
	size  int64
	mtime int64
}

func listFiles(root string, k kind, cutoff time.Time) []job {
	var out []job
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if e != nil && e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if e.IsDir() || filepath.Ext(p) != ".jsonl" {
			return nil
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		out = append(out, job{p, k, info.Size(), info.ModTime().UnixNano()})
		return nil
	})
	return out
}

// readFile parses the new bytes of one file and merges them into the totals.
func (r *Reader) readFile(ctx context.Context, j job, cutoffDay string) {
	r.mu.Lock()
	cur := r.st.Files[j.path]
	r.mu.Unlock()
	if cur != nil && cur.Size == j.size && cur.MTime == j.mtime {
		return
	}
	next := cursor{}
	var shrunk *cursor // the old cursor of a file that started over
	if cur != nil && j.size >= cur.Off {
		next = *cur
		next.Contrib = maps.Clone(cur.Contrib)
	} else if cur != nil {
		shrunk = cur
	}
	f, err := os.Open(j.path)
	if err != nil {
		return
	}
	defer f.Close()

	var events []event
	start := next.Off
	switch j.kind {
	case kindClaude:
		next.Off, events = parseClaude(ctx, f, next.Off, r.loc)
	case kindCodex:
		next.Off, next.Model, next.Prev, events = parseCodex(ctx, f, next.Off, next.Model, next.Prev, r.loc)
	}
	next.Size, next.MTime = j.size, j.mtime
	if ctx.Err() != nil {
		// Stopped early: keep the offset, but make the next refresh resume.
		next.Size = 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.bytesRead += next.Off - start
	if shrunk != nil {
		// The file is shorter than what was read, so it was rewritten. Take its
		// old share out, because the whole file is read again.
		for k, b := range shrunk.Contrib {
			if day, pm, ok := strings.Cut(k, "|"); ok && day >= cutoffDay {
				r.addBucket(day, pm, b, -1)
			}
		}
	}
	for k := range next.Contrib {
		if k < cutoffDay {
			delete(next.Contrib, k)
		}
	}
	for _, ev := range events {
		if ev.day >= cutoffDay {
			r.apply(ev)
			if j.kind == kindCodex {
				if next.Contrib == nil {
					next.Contrib = map[string]Bucket{}
				}
				k := ev.day + "|" + ev.provider + "|" + ev.model
				c := next.Contrib[k]
				c.add(ev.b, 1)
				next.Contrib[k] = c
			}
		}
	}
	r.st.Files[j.path] = &next
	r.dirty = true
}

// apply adds an event. A repeated message replaces the earlier copy when it
// reports more output, because streaming logs grow the count. The caller holds
// r.mu.
func (r *Reader) apply(ev event) {
	pm := ev.provider + "|" + ev.model
	if ev.key != 0 {
		if old, ok := r.seenIdx[ev.key]; ok {
			if ev.b.Out < int64(old.Out) || ev.b.Out == int64(old.Out) && ev.b.tokens() <= old.tokens() {
				return
			}
			r.addBucket(dayStr(old.Day), r.pms[old.PM], old.bucket(), -1)
			delete(r.seenIdx, ev.key)
		}
		// A message that does not fit the compact entry is counted without
		// dedupe, which is the same as a message without an id.
		if i, ok := r.intern(pm); ok && fitsSeen(ev.b) {
			r.seenIdx[ev.key] = seenEntry{
				Day: dayNum(ev.day), PM: i,
				In: uint32(ev.b.In), Out: uint32(ev.b.Out), CW5: uint32(ev.b.CW5), CW1: uint32(ev.b.CW1), CR: uint32(ev.b.CR),
			}
		}
	}
	r.addBucket(ev.day, pm, ev.b, 1)
}

// addBucket adds to the total of a day and a "provider|model" key. The caller
// holds r.mu.
func (r *Reader) addBucket(day, pm string, b Bucket, sign int64) {
	m := r.st.Days[day]
	if m == nil {
		m = map[string]Bucket{}
		r.st.Days[day] = m
	}
	cur := m[pm]
	cur.add(b, sign)
	m[pm] = cur
	r.dirty = true
}

// Spends totals all three ranges at once. It reads memory only, so it is cheap.
func (r *Reader) Spends(now time.Time) map[Range][]Spend {
	return map[Range][]Spend{
		Today:      r.Spend(Today, now),
		Yesterday:  r.Spend(Yesterday, now),
		Last30Days: r.Spend(Last30Days, now),
	}
}

// Spend totals a range per provider, the costliest first. Call Refresh first.
func (r *Reader) Spend(rg Range, now time.Time) []Spend {
	y, m, d := now.In(r.loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, r.loc)
	var from, to time.Time
	switch rg {
	case Today:
		from, to = today, today
	case Yesterday:
		from = today.AddDate(0, 0, -1)
		to = from
	default:
		from, to = today.AddDate(0, 0, -29), today
	}
	lo, hi := dayKey(from, r.loc), dayKey(to, r.loc)

	type acc struct {
		Spend
		unknown, estimated map[string]bool
	}
	by := map[string]*acc{}
	r.mu.Lock()
	for day, models := range r.st.Days {
		if day < lo || day > hi {
			continue
		}
		for k, b := range models {
			provider, model := splitKey(k)
			a := by[provider]
			if a == nil {
				a = &acc{Spend: Spend{Provider: provider}, unknown: map[string]bool{}, estimated: map[string]bool{}}
				by[provider] = a
			}
			usd, q := b.usd(provider, model)
			a.USD += usd
			a.Tokens += b.tokens()
			switch {
			case b.tokens() == 0 && b.Direct == 0:
			case q == Unknown:
				a.unknown[model] = true
			case q == Estimated:
				a.estimated[model] = true
			}
		}
	}
	r.mu.Unlock()

	out := make([]Spend, 0, len(by))
	for _, a := range by {
		if a.Tokens == 0 && a.USD == 0 {
			continue
		}
		a.Unknown, a.Estimated = keys(a.unknown), keys(a.estimated)
		out = append(out, a.Spend)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].USD != out[j].USD {
			return out[i].USD > out[j].USD
		}
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func splitKey(k string) (provider, model string) {
	for i := range len(k) {
		if k[i] == '|' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

func keys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
