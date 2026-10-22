package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"

	"kero/internal/proto"
)

// The Linear workspace, as last read, outlives the daemon in linear.json,
// apart from state.json: it is large, and changes on its own time, while
// the state changes with every status. The daemon keeps it in memory and
// sends it to the app as before; only where it is kept on disk moved.
//
// A state.json of before has the workspace in it: the restore takes it
// from there when linear.json has none, or an older copy (newerLinear),
// and the next save writes it to linear.json, then a state.json without
// it. A daemon of before, run again on the new files, finds no workspace
// in state.json and reads it again from Linear: it loses the cached copy
// only, until that read.

func (d *Daemon) linearPath() string { return filepath.Join(d.dir, "linear.json") }

// loadLinear reads linear.json; nil when there is none, or it does not
// parse.
func (d *Daemon) loadLinear() *proto.LinearState {
	data, err := os.ReadFile(d.linearPath())
	if err != nil {
		return nil
	}
	var lin proto.LinearState
	if json.Unmarshal(data, &lin) != nil {
		return nil
	}
	return &lin
}

// newerLinear is the copy of the workspace read last of two, a when they
// were read at once: one in linear.json and one in a state.json, as after
// a daemon of before ran on them.
func newerLinear(a, b *proto.LinearState) *proto.LinearState {
	if a == nil || b != nil && b.FetchedAt > a.FetchedAt {
		return b
	}
	return a
}

// linearToSaveLocked returns the workspace and what to write of it, when
// it changed since it was written; nil data when it did not. The daemon
// replaces the workspace as it reads it again (linearSet), never changes
// it in place, so a new copy is a new pointer; d.mu is held.
func (d *Daemon) linearToSaveLocked() (*proto.LinearState, []byte) {
	if d.linear == nil || d.linear == d.linearSaved {
		return nil, nil
	}
	data, err := json.MarshalIndent(d.linear, "", "  ")
	if err != nil {
		return nil, nil
	}
	return d.linear, data
}

// linearWritten notes that lin is the copy in linear.json.
func (d *Daemon) linearWritten(lin *proto.LinearState) {
	d.mu.Lock()
	d.linearSaved = lin
	d.mu.Unlock()
}
