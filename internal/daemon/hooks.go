package daemon

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"kero/internal/detect"
	"kero/internal/proto"
)

// A coding agent's hooks (kero hook) tell kerod as its turn starts, as it
// asks for permission and as it ends, with the "report" method: sooner
// and surer than its screen, which the daemon still reads. Their records
// are soft: they set the status at once, but don't silence detection the
// way a program's own report does (declared), since an agent says nothing
// of some ends, as Claude Code of a turn the user stopped with Esc. When
// the screen comes to show otherwise, it tells.

// hookReport applies an agent hook's report to the pane that runs the
// agent's session: the one of p.Pane when it does, else the only one
// that does, as of Codex's app server, which panes share and which runs
// the hooks of all of them.
func (d *Daemon) hookReport(p proto.ReportParams) error {
	switch p.State {
	case "working", "blocked", "done", "error", "clear":
	default:
		return fmt.Errorf("unknown state %q", p.State)
	}
	if p.Agent == "" || p.Session == "" || !appRe.MatchString(p.Agent) {
		return errors.New("no agent session")
	}
	pn := d.hookPane(p.Pane, p.Agent, p.Session)
	if pn == nil {
		return fmt.Errorf("no pane runs %s session %q", p.Agent, p.Session)
	}
	r := proto.Record{ID: p.Agent, State: p.State, App: p.Agent, Progress: -1, Updated: time.Now().UnixMilli()}
	if r.State == "blocked" {
		switch p.Kind {
		case "permission", "question", "auth":
			r.Kind = p.Kind
		}
	}
	r.Msg = plainText(p.Msg, 2048)
	pn.mu.Lock()
	pn.applySoft(r)
	pn.mu.Unlock()
	return nil
}

// hookPane finds the pane whose agent's session is agent's session.
func (d *Daemon) hookPane(ref, agent, session string) *pane {
	d.mu.Lock()
	panes := make([]*pane, 0, len(d.panes))
	for _, p := range d.panes {
		panes = append(panes, p)
	}
	d.mu.Unlock()
	var found []*pane
	for _, p := range panes {
		p.mu.Lock()
		a := p.info.AgentSession
		runs := !p.closed && p.info.Status != proto.Exited && a != nil && a.Agent == agent && a.ID == session
		id := p.info.ID
		p.mu.Unlock()
		if !runs {
			continue
		}
		if id == ref {
			return p
		}
		found = append(found, p)
	}
	if len(found) == 1 {
		return found[0]
	}
	return nil
}

// applySoft takes an agent hook's record, under the agent's own id, so
// that it stands beside what other programs in the pane report; s.mu is
// held.
func (s *pane) applySoft(r proto.Record) {
	if s.soft == nil {
		s.soft = map[string]bool{}
	}
	if r.State == "clear" {
		// Only what the hook said: SessionEnd clears its own, not the
		// reports of the programs beside it.
		if _, ok := s.records[r.ID]; ok {
			delete(s.records, r.ID)
			delete(s.soft, r.ID)
			before := s.info.Status
			s.fromRecords()
			if len(s.records) == 0 && (before == proto.Working || before == proto.Waiting) {
				// What it said goes, and the screen tells again.
				s.setStatus(proto.Idle)
				s.info.Message = ""
				s.seen, s.looked, s.idleFrom = "", false, time.Time{}
				s.d.changed()
			}
		}
		return
	}
	if r.State == "working" {
		// The turn began, whatever the screen showed before it: the
		// screen's next state is news, as its prompt back after Esc.
		s.seen, s.looked, s.idleFrom = "", false, time.Time{}
	}
	s.applyReport(r, false)
	s.soft[r.ID] = true
}

// softLive reports whether an agent's hook said it works or waits, which
// the screen may still overrule; s.mu is held.
func (s *pane) softLive() bool {
	for id, r := range s.records {
		if s.soft[id] && (r.State == "working" || r.State == "blocked") {
			return true
		}
	}
	return false
}

// overrule drops what agents' hooks said that the screen, showing st,
// now contradicts: working or waiting once it shows the agent's prompt,
// a wait once it shows the agent work again (a permission granted), work
// once it shows a wait; and, for st "", all they said of a turn under
// way. It sets no status: the caller does; s.mu is held.
func (s *pane) overrule(st detect.State) {
	changed, blocked := false, false
	for id, r := range s.records {
		if !s.soft[id] {
			continue
		}
		drop := false
		switch st {
		case "", detect.Idle:
			drop = r.State == "working" || r.State == "blocked"
		case detect.Working:
			drop = r.State == "blocked"
		case detect.Blocked:
			drop = r.State == "working"
		}
		if drop {
			blocked = blocked || r.State == "blocked"
			delete(s.records, id)
			delete(s.soft, id)
			changed = true
		}
	}
	if changed {
		// What the dropped told goes with it: its kind of wait, its
		// progress.
		if blocked {
			s.info.Kind = ""
		}
		s.info.Progress = -1
		recs := s.info.Records[:0:0]
		for _, r := range s.info.Records {
			if _, ok := s.records[r.ID]; ok {
				recs = append(recs, r)
			}
		}
		s.info.Records = recs
	}
}

// plainText is text an agent's hook gave, as the sidebar can show it: of
// one line, without controls or what would make it lie about itself, cut
// to at most n bytes.
func plainText(text string, n int) string {
	text = strings.Map(func(c rune) rune {
		switch {
		case c == '\n' || c == '\r' || c == '\t':
			return ' '
		case c < 0x20 || c == 0x7f || c >= 0x80 && c <= 0x9f || c == utf8.RuneError:
			return -1
		}
		return c
	}, text)
	text = strings.Join(strings.Fields(neutralize(text)), " ")
	if len(text) > n {
		i := n
		for i > 0 && !utf8.RuneStart(text[i]) {
			i--
		}
		text = text[:i]
	}
	return text
}
