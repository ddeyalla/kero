package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kero/internal/detect"
	"kero/internal/proto"
)

// The attention of a session is the one thing its row says of all its
// panes: the most urgent of what each runs. Most of it the panes' own
// statuses and records already tell. What they do not, the transcript
// scan reads from the agent's own conversation on disk, which says of a
// call under way what neither the screen nor an agent without hooks for
// it (Codex) says: a call that has run too long with no result waits on
// the user. The scan applies what it finds as the soft records a hook
// would (applySoft), so that the screen keeps its say over it, and
// nothing here changes the protocol.

// attentionState is how a session needs the user, from most urgent down.
type attentionState string

const (
	attentionRunning     attentionState = "running"      // work under way
	attentionWaitingUser attentionState = "waiting-user" // asks of the user
	attentionUnread      attentionState = "unread"       // ended, not seen
	attentionDone        attentionState = "done"         // ended, read or unmarked
	attentionIdle        attentionState = "idle"         // at rest
)

// rank orders the states the fold shows: one waiting beats one working,
// and an unseen end beats rest.
func (st attentionState) rank() int {
	switch st {
	case attentionWaitingUser:
		return 5
	case attentionRunning:
		return 4
	case attentionUnread:
		return 3
	case attentionDone:
		return 2
	default:
		return 1
	}
}

// attentionMsgLen is how much of a message a state's text keeps, as a
// row shows it.
const attentionMsgLen = 80

// attention is one session's: its state, the words that go with it, and
// the pane that gave them.
type attention struct {
	State   attentionState
	Msg     string
	Since   int64
	Pane    string
	Session string
}

// attention folds each session's panes into the state its row shows:
// the most urgent of what its panes say, and of two as urgent, the
// first's. It reads the panes as they are, under their locks.
func (d *Daemon) attention() []attention {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]attention, 0, len(d.sessions))
	for _, s := range d.sessions {
		out = append(out, sessionAttention(s))
	}
	return out
}

// paneRead is what one look at a pane gave the fold: whether it was
// closed, its own attention and its id.
type paneRead struct {
	closed bool
	pa     attention
	id     string
}

// readPane looks at a pane once, under its lock; the same look can then
// serve both the fold and the pane's row (sessionInfo).
func readPane(p *pane) paneRead {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return paneRead{closed: true}
	}
	return paneRead{pa: paneAttention(p), id: p.info.ID}
}

// sessionAttention folds one session's panes into its state: the most
// urgent of what they say, and of two as urgent, the first's. It reads
// them as they are, under their locks; d.mu is held.
func sessionAttention(s *session) attention {
	reads := make(map[*pane]paneRead, len(s.panes))
	for _, p := range s.panes {
		reads[p] = readPane(p)
	}
	return foldAttention(s, reads)
}

// foldAttention folds one session's panes from what a look at each gave,
// in the order of the session's panes; d.mu is held.
func foldAttention(s *session, reads map[*pane]paneRead) attention {
	var a attention
	for i, p := range s.panes {
		r := reads[p]
		if r.closed {
			continue
		}
		if i == 0 || r.pa.State.rank() > a.State.rank() {
			a = attention{State: r.pa.State, Msg: r.pa.Msg, Since: r.pa.Since, Pane: r.id}
		}
	}
	if a.State == "" {
		a.State = attentionIdle
	}
	a.Session = s.id
	return a
}

// protoAttention turns a session's fold into what the state carries.
func protoAttention(a attention) *proto.Attention {
	return &proto.Attention{State: string(a.State), Msg: a.Msg, Since: a.Since, Pane: a.Pane}
}

// paneAttention is a pane's own state, from its status and what its
// records made of it: what its row would show of it alone; s.mu is held.
func paneAttention(p *pane) attention {
	a := attention{State: attentionIdle, Since: p.info.Since}
	if p.info.View != "" {
		// A view never works or fails; its mark is the user's to clear,
		// and it says no turn.
		if p.info.Unread {
			a.State = attentionUnread
		}
		return a
	}
	switch p.info.Status {
	case proto.Working:
		a.State = attentionRunning
		switch {
		case p.info.Progress >= 0:
			a.Msg = fmt.Sprintf("Working · %d%%", p.info.Progress)
		case p.info.Message != "":
			a.Msg = firstLine(p.info.Message, attentionMsgLen)
		default:
			a.Msg = "Working"
		}
	case proto.Waiting:
		a.State = attentionWaitingUser
		a.Msg = paneWaitMsg(p)
	default: // idle, failed, exited
		if p.info.Unread {
			a.State = attentionUnread
			switch p.info.Status {
			case proto.Failed:
				if m := firstLine(p.info.Message, attentionMsgLen); m != "" {
					a.Msg = "Failed: " + m
				} else {
					a.Msg = "Failed"
				}
			case proto.Exited:
				a.Msg = fmt.Sprintf("Exited with code %d", p.info.ExitCode)
			default:
				a.Msg = firstLine(p.info.Message, attentionMsgLen)
				if a.Msg == "" {
					a.Msg = firstLine(p.summary, attentionMsgLen)
				}
				if a.Msg == "" {
					a.Msg = "Turn ended"
				}
			}
		} else {
			switch p.info.Status {
			case proto.Failed:
				a.State = attentionDone
				a.Msg = firstLine(p.info.Message, attentionMsgLen)
			case proto.Idle:
				if p.info.Message != "" {
					a.State = attentionDone
					a.Msg = firstLine(p.info.Message, attentionMsgLen)
				}
			}
		}
	}
	return a
}

// paneWaitMsg words a pane's wait: the message that came with it, or the
// plain kind of it that the screen's rules told; s.mu is held.
func paneWaitMsg(p *pane) string {
	if m := firstLine(p.info.Message, attentionMsgLen); m != "" {
		return m
	}
	switch p.info.Kind {
	case "permission":
		return "Needs permission"
	case "question":
		return "Asked a question"
	case "auth":
		return "Needs authentication"
	}
	return "Needs you"
}

// firstLine is a text's first line, as text shown of another: tidied
// (plainText) and cut to at most n bytes.
func firstLine(text string, n int) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return plainText(text, n)
}

// The transcript scan. It reads the end of an agent's conversation, as
// each agent keeps it, every few ticks: not for what the hooks and the
// screen say (they say it sooner and surer), but for what only the
// conversation tells — that a call has run too long with no result, as
// Codex's do while they ask, and how a turn ended.

// scanRecordID is the id the scan's records stand under, beside a
// program's own reports and its hooks'.
const scanRecordID = "scan"

const (
	// unresolvedCallHold is how long a call the transcript shows under
	// way, with no result after it, may run before the scan takes it
	// for one the user must answer.
	unresolvedCallHold = 45 * time.Second
	// scanTail is how much of a transcript's end a pass reads.
	scanTail = 256 << 10
	// scanMissHold is how long a pass waits before it looks again for a
	// transcript it found none of.
	scanMissHold = 30 * time.Second
	// scanDiscoveryBudget bounds work for a Codex transcript not found yet.
	scanDiscoveryBudget = 256
)

// scanVerdict is what a transcript's tail said, at the pass that read
// it. A state of "" says nothing stands of itself: either the user
// speaks last (the agent waits for them, as ever), or a call under way
// is all there is, whose age a pass judges (callAt).
type scanVerdict struct {
	state  string // "working", "blocked", "done", or ""
	msg    string
	callAt int64
}

// scanVerdictOf is what a verdict says now: a call whose age has passed
// the hold has become a wait on the user, the rest stands as it read.
func scanVerdictOf(v scanVerdict, now int64) (string, string) {
	if v.state != "" || v.callAt == 0 {
		return v.state, v.msg
	}
	tool := plainText(v.msg, attentionMsgLen)
	if v.callAt > now || now-v.callAt < unresolvedCallHold.Milliseconds() {
		return "working", scanWorkMsg(tool)
	}
	return "blocked", scanWaitMsg(tool)
}

// scanWorkMsg names the work the transcript tells of, by the call it
// runs.
func scanWorkMsg(tool string) string {
	if tool == "" {
		return "Working"
	}
	return "Working · " + tool
}

// scanWaitMsg names the wait, by the call it hangs on.
func scanWaitMsg(tool string) string {
	if tool == "" {
		return "Needs you"
	}
	return "Waiting on " + tool
}

// scanPass reads every pane's transcript once. The tick loop calls it
// every few of its ticks, and the daemon's start once, so that the
// panes of the last daemon say at once what ended before it. Two passes
// do not overlap: one that finds another running leaves its work to it.
func (d *Daemon) scanPass(now time.Time) {
	if !d.scanning.CompareAndSwap(false, true) {
		return
	}
	defer d.scanning.Store(false)
	d.mu.Lock()
	panes := make([]*pane, 0, len(d.panes))
	for _, p := range d.panes {
		panes = append(panes, p)
	}
	d.mu.Unlock()
	for _, p := range panes {
		p.scanTranscript(now)
	}
}

// scanJob is what a pass needs of a pane to read its transcript, taken
// under the pane's lock so that the reading itself can go without it.
type scanJob struct {
	agent, dir, id string
	path           string // "" where none is known yet
	key            string // of the file as the pane last read it
	discovery      *transcriptDiscovery
}

// scanRead is what reading a job found: where the transcript is, and, of
// a file that changed, what its tail tells.
type scanRead struct {
	path    string
	key     string
	missed  bool // no file was found
	pending bool // a bounded discovery needs another pass
	gone    bool // the file was known, and is not there
	parsed  bool // the file changed: v and end are of its tail
	v       scanVerdict
	end     scanEnd
}

// scanAgent is the agent session whose transcript the scan may read for
// the pane, or nil where it may not: a pane closed, a view, an agent of
// no two, or a record of another's, a hook's or a program's own, which
// owns the pane's status until it is dismissed (it knows more, and
// sooner); s.mu is held.
func (s *pane) scanAgent() *proto.AgentSession {
	if s.closed || s.isView() {
		return nil
	}
	a := s.info.AgentSession
	if a == nil || a.ID == "" || a.Agent != "claude" && a.Agent != "codex" {
		return nil
	}
	for id := range s.records {
		if id != scanRecordID {
			return nil
		}
	}
	return a
}

// scanTranscript reads the pane's agent's transcript and applies what it
// tells, as the soft record a hook would. It takes s.mu itself, and only
// for what it reads and applies of the pane: the lookup of the file, its
// stat and its tail go without the lock, so that a slow disk never holds
// the pane's output, its hooks or its calls. The hooks and the screen
// say most of a pane's state, and what they have said stands: the scan
// speaks only for a pane with no other word, and takes back what it said
// when the transcript does.
func (s *pane) scanTranscript(now time.Time) bool {
	s.mu.Lock()
	a := s.scanAgent()
	if a == nil {
		old := s.discovery
		s.discovery = nil
		s.mu.Unlock()
		old.close()
		return false
	}
	// Agent hooks can reuse a pane for another conversation. A cached path
	// or verdict of the previous conversation must never carry over.
	if s.scanAgentID != "" && s.scanAgentID != a.Agent+":"+a.ID {
		s.applySoft(proto.Record{ID: scanRecordID, State: "clear"})
		old := s.discovery
		s.discovery = nil
		s.scanAgentID = a.Agent + ":" + a.ID
		s.transcriptPath, s.scanKey, s.scanTold, s.scanToldMsg = "", "", "", ""
		s.scanV, s.scanEnd, s.scanTurn, s.scanToldTurn = scanVerdict{}, scanEnd{}, 0, 0
		s.scanMissed = time.Time{}
		s.mu.Unlock()
		old.close()
		s.mu.Lock()
		a = s.scanAgent()
		if a == nil || s.scanAgentID != a.Agent+":"+a.ID {
			s.mu.Unlock()
			return false
		}
	}
	s.scanAgentID = a.Agent + ":" + a.ID
	if a.Agent == "codex" && s.transcriptPath == "" && s.discovery == nil {
		home, err := os.UserHomeDir()
		if err == nil {
			s.discovery = newTranscriptDiscovery(filepath.Join(home, ".codex", "sessions"))
		}
	}
	job := scanJob{agent: a.Agent, dir: s.info.Dir, id: a.ID, path: s.transcriptPath, key: s.scanKey, discovery: s.discovery}
	if job.path == "" && !s.scanMissed.IsZero() && now.Sub(s.scanMissed) < scanMissHold {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	read := job.read(now)

	s.mu.Lock()
	defer s.mu.Unlock()
	// The pane may have changed while the file was read: closed, given
	// another record or session, or read by a pass of its own.
	if a := s.scanAgent(); a == nil || a.ID != job.id || a.Agent != job.agent || s.scanKey != job.key {
		return false
	}
	return s.scanApply(now, job, read)
}

// read finds the transcript, as far as the job knows it, and reads its
// tail if the file changed since the pane's last pass; the pane's lock
// is not held.
func (j scanJob) read(now time.Time) scanRead {
	r := scanRead{path: j.path}
	if r.path == "" {
		if j.discovery != nil {
			r.path, r.pending = j.discovery.next(j.id, scanDiscoveryBudget)
			if !r.pending {
				j.discovery.close()
			}
			if r.pending {
				return r
			}
		} else {
			r.path = findTranscript(j.agent, j.dir, j.id)
		}
		if r.path == "" {
			r.missed = true
			return r
		}
	}
	st, err := os.Stat(r.path)
	if err != nil {
		r.gone = true
		return r
	}
	if r.key = scanKeyOf(st); r.key != j.key {
		data, at := tailLines(r.path, scanTail)
		r.v, r.end = parseScanTail(j.agent, data, now)
		if r.end.present {
			r.end.offset += at
		}
		r.parsed = true
	}
	return r
}

// scanApply keeps what a read found and says what it tells of the pane;
// s.mu is held.
func (s *pane) scanApply(now time.Time, job scanJob, read scanRead) bool {
	a := s.info.AgentSession
	switch {
	case read.pending:
		return false
	case read.missed:
		s.discovery = nil
		s.scanMissed = now
		return false
	case read.gone:
		s.discovery = nil
		// Gone, as of a session the user cleared: look again later.
		s.transcriptPath, s.scanMissed = "", now
		return false
	}
	s.transcriptPath, s.scanMissed = read.path, time.Time{}
	s.discovery = nil
	if read.parsed {
		// Lines are new: keep what the tail told, so that a pass with
		// none says again what this one said. A tail that tells another
		// thing, or another end, is another turn's: what was said of
		// the one is no news of it.
		s.scanKey = read.key
		if read.v != s.scanV || read.end != s.scanEnd {
			s.scanTurn++
		}
		s.scanV, s.scanEnd = read.v, read.end
	}
	state, msg := scanVerdictOf(s.scanV, now.UnixMilli())
	if state == "" {
		// The transcript no longer says the pane works or waits: what
		// the scan said of one goes, so that a turn the user stopped
		// with Esc, or its answer, does not hold the pane as working.
		// A done it said stands for the user's first key, as a hook's.
		if s.scanTold == "working" || s.scanTold == "blocked" {
			s.applySoft(proto.Record{ID: scanRecordID, State: "clear", App: a.Agent, Progress: -1, Updated: now.UnixMilli()})
			s.scanTold, s.scanToldMsg = "", ""
		}
		return false
	}
	// A turn's end is told once, by which end it is (scanTurn), not by
	// its words: two turns that end on the same last line ("Done.") are
	// two ends, and the user's key between them does not make the first
	// the second. The user's key leaves what was told as it is, so that
	// an end the user has read is not told again.
	if state == s.scanTold && msg == s.scanToldMsg && s.scanTurn == s.scanToldTurn {
		return false // said, and it stands
	}
	// The work the scan itself said of stands, until the transcript says
	// otherwise or the screen overrules it (overrule).
	ownWork := s.soft[scanRecordID] && s.records[scanRecordID].State == "working"
	// What the screen holds comes first of a working pane: only a wait
	// may join it (a call gone old under the work), or the end of work
	// the scan itself began; and nothing at all of a pane that waits or
	// has failed, whose state a key of the user's, or the program's own
	// word, ends — but for a wait the scan itself said, which it may
	// take back when its call ended and the screen has said no wait of
	// its own.
	switch s.info.Status {
	case proto.Working:
		if state != "blocked" && !(state == "done" && ownWork) {
			return false
		}
	case proto.Waiting:
		if !s.soft[scanRecordID] || s.seen == detect.Blocked {
			return false
		}
	case proto.Failed, proto.Exited:
		return false
	}
	// A done of a pane the user has seen is no news: the scan does not
	// mark a pane unread that no other signal marked, which a transcript
	// older than the daemon would do to every pane it restored. Work the
	// scan began is the exception, whose end nothing else tells.
	if state == "done" && !s.info.Unread && !ownWork {
		return false
	}
	if (state == "working" || state == "blocked") && s.proc == nil {
		return false // no program runs, for all the transcript says
	}
	s.applySoft(proto.Record{ID: scanRecordID, State: state, App: a.Agent, Progress: -1, Msg: msg, Updated: now.UnixMilli()})
	s.scanTold, s.scanToldMsg, s.scanToldTurn = state, msg, s.scanTurn
	return true
}

// scanKeyOf is a file's time of change and size, by which a pass tells
// the lines it read from the ones before.
func scanKeyOf(st os.FileInfo) string {
	return strconv.FormatInt(st.ModTime().UnixNano(), 36) + ":" + strconv.FormatInt(st.Size(), 36)
}

// scanSessionRe is the shape of an agent's session id, as a file name's
// worth of it: a transcript path is built of it, so nothing that names a
// directory may pass.
var scanSessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// findTranscript finds where an agent keeps the session's conversation,
// by how each names its files; "" when it has none, yet.
func findTranscript(agent, dir, session string) string {
	if session == "" || !scanSessionRe.MatchString(session) {
		return ""
	}
	switch agent {
	case "claude":
		return claudeTranscript(dir, session)
	case "codex":
		return codexTranscript(session)
	}
	return ""
}

// claudeProjectDir is how Claude Code names a transcript's directory, by
// the path the session ran in: what is not a letter or a digit becomes
// a dash.
func claudeProjectDir(dir string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '-'
	}, dir)
}

// claudeTranscript is ~/.claude/projects/<the directory, flattened>/
// <session>.jsonl.
func claudeTranscript(dir, session string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := filepath.Join(home, ".claude", "projects", claudeProjectDir(dir), session+".jsonl")
	if st, err := os.Stat(name); err == nil && !st.IsDir() {
		return name
	}
	// The project may be named otherwise, or the session have run in a
	// directory the pane left: the newest of any project's that carries
	// the session's id.
	matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", session+".jsonl"))
	var best string
	var bestAt time.Time
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && !st.IsDir() {
			if best == "" || st.ModTime().After(bestAt) {
				best, bestAt = m, st.ModTime()
			}
		}
	}
	return best
}

// codexTranscript is where Codex keeps a session's rollout:
// ~/.codex/sessions/<year>/<month>/<day>/rollout-…-<session>.jsonl, or
// any nesting of its dates.
func codexTranscript(session string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	search := newTranscriptDiscovery(filepath.Join(home, ".codex", "sessions"))
	defer search.close()
	path, _ := search.next(session, scanDiscoveryBudget)
	return path
}

// tailLines reads at most n bytes, of complete lines only, and returns
// their absolute source offset. A moving read window must never change a
// turn-end event's identity, and a partial final line is not an event.
func tailLines(path string, n int64) ([]byte, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil, 0
	}
	at := st.Size() - n
	if at < 0 {
		at = 0
	}
	data := make([]byte, st.Size()-at)
	read, err := f.ReadAt(data, at)
	if read < len(data) {
		data = data[:read]
	}
	if err != nil && err != io.EOF {
		return nil, 0
	}
	if at > 0 {
		// The read began mid-line: its partial one is not a line.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
			at += int64(i + 1)
		} else {
			return nil, 0
		}
	}
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		return data[:i+1], at
	}
	return nil, 0 // no whole line in it
}

// parseScan reads a transcript's tail, by the agent that wrote it, and
// says what its end tells; now is the pass's time, for lines that say
// none of their own.
func parseScan(agent string, data []byte, now time.Time) scanVerdict {
	v, _ := parseScanTail(agent, data, now)
	return v
}

// parseScanTail returns the verdict and the last turn-end identity. Its
// offset is relative to data; the file reader makes it absolute.
func parseScanTail(agent string, data []byte, now time.Time) (scanVerdict, scanEnd) {
	switch agent {
	case "claude":
		return claudeScanTail(data, now)
	case "codex":
		return codexScanTail(data, now)
	}
	return scanVerdict{}, scanEnd{}
}

// parsedScan is what a transcript's tail says, as a scan reads it
// through: the agent's own last text, whether the user spoke after it,
// how the turn was told to end, and the calls under way by their id.
type parsedScan struct {
	lastText string
	userLast bool
	turnEnd  bool
	started  bool   // codex: a turn was told begun
	done     string // codex: the text a turn ended with
	doneSeen bool   // codex: that a turn was told ended
	aborted  bool   // codex: the turn was stopped
	pending  map[string]scanCall
	// calls counts the calls the tail added, so that two of an age pick
	// the one written first, not whichever a map's walk finds first.
	calls int
	// end identifies the last completion, independent of how many earlier
	// completions still fit in the bounded read window.
	end scanEnd
}

// scanEnd is a completion's stable source identity. Absolute byte position
// distinguishes identical lines even when timestamps or event IDs repeat.
type scanEnd struct {
	timestamp string
	id        string
	offset    int64
	present   bool
}

// scanCall is a call the transcript shows under way: what it runs, when
// its line was written, and which call of the tail it is.
type scanCall struct {
	name string
	at   int64
	n    int
}

// pendingOldest is the call of the tail's end that has waited longest,
// when any did: the one the user is on, if the scan must say.
func (ps *parsedScan) pendingOldest() (scanCall, bool) {
	var best scanCall
	have := false
	for _, c := range ps.pending {
		if !have || c.at < best.at || c.at == best.at && c.n < best.n {
			best, have = c, true
		}
	}
	return best, have
}

// scanLine is a transcript line's head: what kind it is, when, and its
// body, which each agent shapes its own way.
type scanLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	Timestamp string          `json:"timestamp"`
	ID        json.RawMessage `json:"id"`
	UUID      json.RawMessage `json:"uuid"`
	IsMeta    bool            `json:"isMeta"`
	Message   json.RawMessage `json:"message"`
	Payload   json.RawMessage `json:"payload"`
}

// claudeMsg is a Claude Code line's message: its content a plain
// string, or a list of its parts.
type claudeMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// claudeItem is a part of one: the agent's text, a call of a tool, or
// the result of one.
type claudeItem struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
}

// content walks a message's parts.
func (m claudeMsg) content(fn func(claudeItem)) {
	var text string
	if len(m.Content) > 0 && json.Unmarshal(m.Content, &text) == nil {
		fn(claudeItem{Type: "text", Text: text})
		return
	}
	var items []claudeItem
	if json.Unmarshal(m.Content, &items) == nil {
		for _, c := range items {
			fn(c)
		}
	}
}

// claudeScan reads the tail of a Claude Code transcript: its assistant
// lines say what it wrote and what it runs; its user lines, of results
// and of the user's own word; its system lines, of a turn's end.
func claudeScan(data []byte, now time.Time) scanVerdict {
	v, _ := claudeScanTail(data, now)
	return v
}

// claudeScanTail is claudeScan, with the last completion's source identity.
func claudeScanTail(data []byte, now time.Time) (scanVerdict, scanEnd) {
	ps := &parsedScan{pending: map[string]scanCall{}}
	var offset int64
	for _, line := range bytes.Split(data, []byte("\n")) {
		start := offset
		offset += int64(len(line) + 1)
		if len(line) == 0 {
			continue
		}
		var l scanLine
		if json.Unmarshal(line, &l) != nil {
			continue
		}
		at := scanAt(l.Timestamp, now)
		switch l.Type {
		case "assistant":
			// The agent acted: the user's word before it is answered,
			// and a turn's end before it is past.
			ps.userLast, ps.turnEnd = false, false
			var m claudeMsg
			if json.Unmarshal(l.Message, &m) != nil {
				continue
			}
			m.content(func(c claudeItem) {
				switch c.Type {
				case "text":
					if c.Text != "" {
						ps.lastText = c.Text
					}
				case "tool_use", "server_tool_use":
					if c.ID != "" {
						ps.calls++
						ps.pending[c.ID] = scanCall{name: c.Name, at: at, n: ps.calls}
					}
				}
			})
		case "user":
			var m claudeMsg
			if json.Unmarshal(l.Message, &m) != nil {
				continue
			}
			m.content(func(c claudeItem) {
				switch {
				case strings.HasSuffix(c.Type, "_result"):
					// A result of a call: it is over, whatever its kind.
					if c.ToolUseID != "" {
						delete(ps.pending, c.ToolUseID)
					}
				case c.Type == "text":
					if !l.IsMeta {
						// The user's own word: nothing runs past it, a
						// turn stopped with Esc as much as an answer.
						ps.userLast = true
						clear(ps.pending)
					}
				}
			})
		case "system":
			switch l.Subtype {
			case "turn_duration", "stop_hook_summary":
				// The turn ended, its calls with it.
				ps.turnEnd = true
				ps.end = scanEnd{timestamp: l.Timestamp, id: string(l.UUID) + ":" + string(l.ID), offset: start, present: true}
				clear(ps.pending)
			}
		}
	}
	return ps.claudeEnd(), ps.end
}

// claudeEnd is what the tail's end tells, its calls first: one under
// way with no result after it, then the user's last word, then a turn's
// end told, then work the agent said of.
func (ps *parsedScan) claudeEnd() scanVerdict {
	if call, ok := ps.pendingOldest(); ok {
		return scanVerdict{msg: call.name, callAt: call.at}
	}
	if ps.userLast {
		return scanVerdict{}
	}
	if ps.turnEnd {
		return scanVerdict{state: "done", msg: firstLine(ps.lastText, attentionMsgLen)}
	}
	if ps.lastText != "" {
		return scanVerdict{state: "working"}
	}
	return scanVerdict{}
}

// codexPayload is the body of a Codex rollout line, by its kind: what
// happened, of the turn, the call, or the message.
type codexPayload struct {
	Type             string `json:"type"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Role             string `json:"role"`
	Message          string `json:"message"`
	LastAgentMessage string `json:"last_agent_message"`
}

// codexScan reads the tail of a Codex rollout: its events say a turn
// began, ended, or was stopped; its response items say of calls and of
// messages.
func codexScan(data []byte, now time.Time) scanVerdict {
	v, _ := codexScanTail(data, now)
	return v
}

// codexScanTail is codexScan, with the last completion's source identity.
func codexScanTail(data []byte, now time.Time) (scanVerdict, scanEnd) {
	ps := &parsedScan{pending: map[string]scanCall{}}
	var offset int64
	for _, line := range bytes.Split(data, []byte("\n")) {
		start := offset
		offset += int64(len(line) + 1)
		if len(line) == 0 {
			continue
		}
		var l scanLine
		if json.Unmarshal(line, &l) != nil {
			continue
		}
		var p codexPayload
		if json.Unmarshal(l.Payload, &p) != nil {
			continue
		}
		at := scanAt(l.Timestamp, now)
		switch l.Type {
		case "event_msg":
			switch p.Type {
			case "task_started":
				// A turn began: the user's word before it is answered,
				// and no call of a turn before runs.
				ps.started, ps.doneSeen, ps.aborted = true, false, false
				ps.done, ps.userLast = "", false
				clear(ps.pending)
			case "task_complete":
				ps.done, ps.doneSeen = p.LastAgentMessage, true
				ps.end = scanEnd{timestamp: l.Timestamp, id: string(l.UUID) + ":" + string(l.ID), offset: start, present: true}
				ps.started, ps.aborted = false, false
			case "turn_aborted":
				// The turn was stopped: its calls are over.
				ps.started, ps.doneSeen, ps.aborted = false, false, true
				ps.done, ps.userLast = "", false
				clear(ps.pending)
			}
		case "response_item":
			switch p.Type {
			case "function_call", "custom_tool_call":
				if p.CallID != "" {
					ps.calls++
					ps.pending[p.CallID] = scanCall{name: p.Name, at: at, n: ps.calls}
				}
			case "function_call_output", "custom_tool_call_output":
				if p.CallID != "" {
					delete(ps.pending, p.CallID)
				}
			case "message":
				switch p.Role {
				case "user":
					ps.userLast = true
				case "assistant":
					ps.userLast = false
				}
			}
		}
	}
	return ps.codexEnd(), ps.end
}

// codexEnd is what the tail's end tells, as a Claude one does: its
// calls first, then the user's word, then how the turn ended, then a
// turn under way with no end told.
func (ps *parsedScan) codexEnd() scanVerdict {
	if call, ok := ps.pendingOldest(); ok {
		return scanVerdict{msg: call.name, callAt: call.at}
	}
	if ps.userLast || ps.aborted {
		return scanVerdict{}
	}
	if ps.doneSeen {
		return scanVerdict{state: "done", msg: firstLine(ps.done, attentionMsgLen)}
	}
	if ps.started {
		return scanVerdict{state: "working"}
	}
	return scanVerdict{}
}

// scanAt is when a transcript's line was written, as the pass can use
// it: the line's own time, or the pass's when it says none one reads.
func scanAt(ts string, now time.Time) int64 {
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t.UnixMilli()
	}
	return now.UnixMilli()
}
