package cost

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"time"
)

// cacheFile holds the totals in a compact binary format. The first version
// wrote JSON to cost-cache.json; that file is removed on the first save.
const (
	cacheFile   = "cost-cache.bin"
	legacyCache = "cost-cache.json"
	cacheMagic  = "SCC"
)

var errCache = errors.New("bad cost cache")

// snapshot is a copy of the state that can be encoded without the lock.
type snapshot struct {
	st    state
	pms   []string
	seen  []snapSeen
	files map[string]*cursor
}

type snapSeen struct {
	key uint64
	e   seenEntry
}

// load restores the cache. A missing, corrupt or old cache is ignored.
func (r *Reader) load() {
	if r.dir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(r.dir, cacheFile))
	if err != nil {
		return
	}
	st, pms, seen, err := decodeCache(data)
	if err != nil {
		return
	}
	idx := make(map[string]uint16, len(pms))
	for i, p := range pms {
		idx[p] = uint16(i)
	}
	r.st, r.pms, r.pmIdx, r.seenIdx = st, pms, idx, seen
	r.lastSave = time.Now()
}

// Flush writes the cache now if it changed. Call it when the reader is done.
func (r *Reader) Flush() error { return r.save() }

// save writes the cache when it changed. It copies the state under the lock
// and encodes and writes outside it. It writes a temporary file and renames
// it, so a crash leaves the old cache.
func (r *Reader) save() error {
	if r.dir == "" {
		return nil
	}
	r.saveMu.Lock()
	defer r.saveMu.Unlock()

	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return nil
	}
	snap := r.snapshot()
	r.dirty = false
	r.lastSave = time.Now()
	r.mu.Unlock()

	err := writeCache(r.dir, encodeCache(snap))
	if err != nil {
		r.mu.Lock()
		r.dirty = true
		r.mu.Unlock()
	}
	return err
}

// snapshot copies the state. Cursors are never changed after they are stored,
// so they are shared. The caller holds r.mu.
func (r *Reader) snapshot() snapshot {
	s := snapshot{
		st:    state{Version: r.st.Version, DB: r.st.DB},
		pms:   r.pms[:len(r.pms):len(r.pms)],
		seen:  make([]snapSeen, 0, len(r.seenIdx)),
		files: make(map[string]*cursor, len(r.st.Files)),
	}
	s.st.Days = make(map[string]map[string]Bucket, len(r.st.Days))
	for day, m := range r.st.Days {
		c := make(map[string]Bucket, len(m))
		for k, b := range m {
			c[k] = b
		}
		s.st.Days[day] = c
	}
	for p, c := range r.st.Files {
		s.files[p] = c
	}
	for k, e := range r.seenIdx {
		s.seen = append(s.seen, snapSeen{k, e})
	}
	return s
}

func writeCache(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, cacheFile+".*") // created 0600
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, cacheFile)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Remove(filepath.Join(dir, legacyCache))
	return nil
}

func putStr(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func putBucket(b []byte, k Bucket) []byte {
	for _, v := range [...]int64{k.In, k.Out, k.CW5, k.CW1, k.CR} {
		b = binary.AppendVarint(b, v)
	}
	return binary.LittleEndian.AppendUint64(b, math.Float64bits(k.Direct))
}

// encodeCache writes: magic, version, a CRC of the rest, then the model keys,
// files, days and seen messages.
func encodeCache(s snapshot) []byte {
	b := make([]byte, 0, 64+len(s.seen)*20)
	b = binary.AppendVarint(b, s.st.DB)
	b = binary.AppendUvarint(b, uint64(len(s.pms)))
	for _, p := range s.pms {
		b = putStr(b, p)
	}
	b = binary.AppendUvarint(b, uint64(len(s.files)))
	for p, c := range s.files {
		b = putStr(b, p)
		b = binary.AppendVarint(b, c.Size)
		b = binary.AppendVarint(b, c.MTime)
		b = binary.AppendVarint(b, c.Off)
		b = putStr(b, c.Model)
		for _, v := range c.Prev {
			b = binary.AppendVarint(b, v)
		}
		b = binary.AppendUvarint(b, uint64(len(c.Contrib)))
		for k, v := range c.Contrib {
			b = putStr(b, k)
			b = putBucket(b, v)
		}
	}
	b = binary.AppendUvarint(b, uint64(len(s.st.Days)))
	for day, m := range s.st.Days {
		b = putStr(b, day)
		b = binary.AppendUvarint(b, uint64(len(m)))
		for k, v := range m {
			b = putStr(b, k)
			b = putBucket(b, v)
		}
	}
	b = binary.AppendUvarint(b, uint64(len(s.seen)))
	for _, e := range s.seen {
		b = binary.LittleEndian.AppendUint64(b, e.key)
		b = binary.AppendUvarint(b, uint64(e.e.Day))
		b = binary.AppendUvarint(b, uint64(e.e.PM))
		for _, v := range [...]uint32{e.e.In, e.e.Out, e.e.CW5, e.e.CW1, e.e.CR} {
			b = binary.AppendUvarint(b, uint64(v))
		}
	}
	out := make([]byte, 0, len(b)+16)
	out = append(out, cacheMagic...)
	out = binary.AppendUvarint(out, stateVersion)
	out = binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(b))
	return append(out, b...)
}

// dec reads the cache. The first error sticks, and later reads return zeros.
type dec struct {
	b   []byte
	err error
}

func (d *dec) fail() { d.err = errCache; d.b = nil }

func (d *dec) uvarint() uint64 {
	v, n := binary.Uvarint(d.b)
	if n <= 0 {
		d.fail()
		return 0
	}
	d.b = d.b[n:]
	return v
}

func (d *dec) varint() int64 {
	v, n := binary.Varint(d.b)
	if n <= 0 {
		d.fail()
		return 0
	}
	d.b = d.b[n:]
	return v
}

func (d *dec) u64() uint64 {
	if len(d.b) < 8 {
		d.fail()
		return 0
	}
	v := binary.LittleEndian.Uint64(d.b)
	d.b = d.b[8:]
	return v
}

// count reads an item count, and rejects one that cannot fit in what is left
// (every item takes at least one byte).
func (d *dec) count() int {
	n := d.uvarint()
	if n > uint64(len(d.b)) {
		d.fail()
		return 0
	}
	return int(n)
}

func (d *dec) str() string {
	n := d.count()
	if d.err != nil {
		return ""
	}
	s := string(d.b[:n])
	d.b = d.b[n:]
	return s
}

func (d *dec) bucket() Bucket {
	var k Bucket
	k.In, k.Out, k.CW5, k.CW1, k.CR = d.varint(), d.varint(), d.varint(), d.varint(), d.varint()
	k.Direct = math.Float64frombits(d.u64())
	return k
}

func decodeCache(data []byte) (state, []string, map[uint64]seenEntry, error) {
	var zero state
	head := len(cacheMagic)
	if len(data) < head || !bytes.Equal(data[:head], []byte(cacheMagic)) {
		return zero, nil, nil, errCache
	}
	d := &dec{b: data[head:]}
	if d.uvarint() != stateVersion || len(d.b) < 4 {
		return zero, nil, nil, errCache
	}
	sum := binary.LittleEndian.Uint32(d.b)
	d.b = d.b[4:]
	if crc32.ChecksumIEEE(d.b) != sum {
		return zero, nil, nil, errCache
	}

	st := state{Version: stateVersion, Files: map[string]*cursor{}, Days: map[string]map[string]Bucket{}}
	st.DB = d.varint()
	pms := make([]string, d.count())
	for i := range pms {
		pms[i] = d.str()
	}
	for range d.count() {
		p := d.str()
		c := &cursor{Size: d.varint(), MTime: d.varint(), Off: d.varint(), Model: d.str()}
		for i := range c.Prev {
			c.Prev[i] = d.varint()
		}
		if n := d.count(); n > 0 {
			c.Contrib = make(map[string]Bucket, n)
			for range n {
				k := d.str()
				c.Contrib[k] = d.bucket()
			}
		}
		st.Files[p] = c
	}
	for range d.count() {
		day := d.str()
		m := map[string]Bucket{}
		for range d.count() {
			k := d.str()
			m[k] = d.bucket()
		}
		st.Days[day] = m
	}
	n := d.count()
	seen := make(map[uint64]seenEntry, n)
	for range n {
		key := d.u64()
		e := seenEntry{Day: uint16(d.uvarint()), PM: uint16(d.uvarint())}
		e.In, e.Out, e.CW5, e.CW1, e.CR = uint32(d.uvarint()), uint32(d.uvarint()), uint32(d.uvarint()), uint32(d.uvarint()), uint32(d.uvarint())
		if int(e.PM) >= len(pms) {
			d.fail()
		}
		seen[key] = e
	}
	if d.err != nil || len(d.b) != 0 {
		return zero, nil, nil, errCache
	}
	return st, pms, seen, nil
}
