package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// slotMS is the width of the time slots that OpenCode totals are grouped by.
// Fifteen minutes lines up with every time zone offset, so the slots can be
// assigned to local days without the sqlite3 tool knowing the zone.
const slotMS = 15 * 60 * 1000

func openCodeDB() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "opencode", "opencode.db")
}

type openCodeRow struct {
	Slot   int64   `json:"slot"`
	Model  string  `json:"model"`
	Cost   float64 `json:"cost"`
	In     int64   `json:"i"`
	Out    int64   `json:"o"`
	Reason int64   `json:"re"`
	CR     int64   `json:"cr"`
	CW     int64   `json:"cw"`
}

// readOpenCode totals OpenCode's own cost and token fields. The database is
// SQLite, and go.mod has no SQLite driver, so it runs the system sqlite3 tool
// in read-only mode. Without the tool or the database it does nothing. The
// totals of the kept days are recomputed whole, because OpenCode updates a
// message while it streams.
func (r *Reader) readOpenCode(ctx context.Context, now, cutoff time.Time, cutoffDay string) {
	db := openCodeDB()
	if db == "" {
		return
	}
	stamp := int64(0)
	for _, suffix := range []string{"", "-wal"} {
		if info, err := os.Stat(db + suffix); err == nil {
			stamp += info.ModTime().UnixNano() + info.Size()
		} else if suffix == "" {
			return
		}
	}
	r.mu.Lock()
	unchanged := r.st.DB == stamp
	r.mu.Unlock()
	if unchanged {
		return
	}
	tool := sqliteTool()
	if tool == "" {
		return
	}
	query := fmt.Sprintf(`select time_created/%d as slot,
 coalesce(json_extract(data,'$.providerID'),'')||'/'||coalesce(json_extract(data,'$.modelID'),'') as model,
 sum(coalesce(json_extract(data,'$.cost'),0)) as cost,
 sum(coalesce(json_extract(data,'$.tokens.input'),0)) as i,
 sum(coalesce(json_extract(data,'$.tokens.output'),0)) as o,
 sum(coalesce(json_extract(data,'$.tokens.reasoning'),0)) as re,
 sum(coalesce(json_extract(data,'$.tokens.cache.read'),0)) as cr,
 sum(coalesce(json_extract(data,'$.tokens.cache.write'),0)) as cw
from message where json_extract(data,'$.role')='assistant' and time_created >= %d
group by slot, model;`, slotMS, cutoff.UnixMilli())
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, tool, "-readonly", "-json", "-cmd", ".timeout 2000", db, query).Output()
	if err != nil {
		return
	}
	var rows []openCodeRow
	if len(strings.TrimSpace(string(out))) > 0 && json.Unmarshal(out, &rows) != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for day, models := range r.st.Days {
		for k := range models {
			if p, _ := splitKey(k); p == "opencode" {
				delete(models, k)
			}
		}
		if len(models) == 0 {
			delete(r.st.Days, day)
		}
	}
	for _, row := range rows {
		day := dayKey(time.UnixMilli(row.Slot*slotMS), r.loc)
		if day < cutoffDay {
			continue
		}
		r.addBucket(day, "opencode|"+row.Model, Bucket{
			In: row.In, Out: row.Out + row.Reason, CR: row.CR, CW5: row.CW, Direct: row.Cost,
		}, 1)
	}
	r.st.DB = stamp
	r.dirty = true
}

// sqliteTool finds the sqlite3 tool. It prefers the one that macOS ships, so a
// copy earlier on PATH cannot change what runs.
func sqliteTool() string {
	const system = "/usr/bin/sqlite3"
	if info, err := os.Stat(system); err == nil && !info.IsDir() {
		return system
	}
	if p, err := exec.LookPath("sqlite3"); err == nil {
		return p
	}
	return ""
}
