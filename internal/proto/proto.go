// Package proto is the wire protocol between kerod, the daemon that owns
// every session's pseudo-terminal, and its clients: the app and the kero
// CLI.
//
// A client connects to the daemon's Unix socket and sends one JSON line,
// a Hello. A control connection then exchanges JSON lines: requests,
// their responses, and, once subscribed, events. An attach connection
// streams one session's terminal as binary frames (see frame.go).
package proto

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// Version changes whenever the protocol does: a client refuses a daemon of
// another version, which it offers to restart.
const Version = 2

// Hello opens a connection.
type Hello struct {
	Kind    string `json:"kind"` // "control" or "attach"
	Version int    `json:"version"`
	// Attach only.
	Session string `json:"session,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

// Welcome answers a Hello.
type Welcome struct {
	Version int   `json:"version"`
	PID     int   `json:"pid"`
	Started int64 `json:"started"` // unix ms
	// App is the version of Kero that kerod runs (App.Version): after an
	// update it is the version before, until kerod restarts.
	App   string `json:"app,omitempty"`
	Error string `json:"error,omitempty"`
	// Attach only: the size of the snapshot that follows.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
}

// Request is a call on a control connection.
type Request struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// Message is what the daemon writes on a control connection: a response
// (ID set) or an event (Event set).
type Message struct {
	ID     int64  `json:"id,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`

	Event string `json:"event,omitempty"` // "state", "select", "action", "notice"
	State *State `json:"state,omitempty"`
	// Select asks the app to show a session ("kero focus").
	Select string `json:"select,omitempty"`
	// Action asks the app to run one of its actions ("kero do"), with
	// Args, when the user allows Remote Control.
	Action string            `json:"action,omitempty"`
	Args   map[string]string `json:"args,omitempty"`
	// Notice is a program's desktop notification, to show as it is.
	Notice *Notice `json:"notice,omitempty"`
}

// Notice is a desktop notification a program in a session sent.
type Notice struct {
	Session string `json:"session"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body"`
}

// Status is what a session is doing, as the sidebar shows it.
type Status string

const (
	// Idle: at a prompt, or a program waiting quietly.
	Idle Status = "idle"
	// Working: a program is busy, as an agent thinking or a build.
	Working Status = "working"
	// Waiting: a program needs the user: it asked a question, rang the
	// bell or posted a notification.
	Waiting Status = "waiting"
	// Failed: a program reported it failed and stopped (OSC 7501 error),
	// until the user looks or types.
	Failed Status = "error"
	// Exited: the session's shell ended.
	Exited Status = "exited"
)

// Record is a program's report of its status, as OSC 7501 (program
// status) sends it: one per id, the root's id "".
type Record struct {
	ID    string `json:"id,omitempty"`
	State string `json:"state"`          // idle, working, done, blocked, error
	Kind  string `json:"kind,omitempty"` // blocked: permission, question, auth
	// Progress is 0–100, or -1 when unknown.
	Progress int    `json:"progress"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Msg      string `json:"msg,omitempty"`
	Updated  int64  `json:"updated"`
}

// State is everything the daemon knows, which it sends whole on every
// change: there are tens of sessions, not thousands.
type State struct {
	Projects []Project `json:"projects"`
	Sessions []Session `json:"sessions"`
	// Viewed is the session a client shows in a focused window.
	Viewed  string `json:"viewed,omitempty"`
	Started int64  `json:"started"` // unix ms, the daemon's start
	// Linear is the workspace the daemon reads for Linear: its issues,
	// projects and milestones, as last read. Nil when there is no key,
	// or when the workspace was never read.
	Linear *LinearState `json:"linear,omitempty"`
}

// LinearStatus is what a Linear workspace is to the daemon: whether it
// was read, and why not when it was not.
const (
	LinearOK          = "ok"
	LinearNoToken     = "no_token"
	LinearAuthFailed  = "auth_failed"
	LinearRateLimited = "rate_limited"
	LinearUnreachable = "unreachable"
	LinearDisabled    = "disabled"
)

// LinearState is a Linear workspace as the daemon last read it: the
// team's states, its projects with their milestones, and the issues.
type LinearState struct {
	// Status is one of the LinearStatus constants.
	Status string `json:"status"`
	// Message says what went wrong, when something did.
	Message  string `json:"message,omitempty"`
	Team     string `json:"team,omitempty"`
	TeamName string `json:"teamName,omitempty"`
	// States are the team's workflow states, in the order the team
	// puts them in.
	States   []LinearWorkflowState `json:"states,omitempty"`
	Projects []LinearProject       `json:"projects,omitempty"`
	Issues   []LinearIssue         `json:"issues,omitempty"`
	// FetchedAt is when the workspace was read (unix ms).
	FetchedAt int64 `json:"fetchedAt,omitempty"`
}

// LinearWorkflowState is a state of a team's workflow: Todo, In
// Progress, Done. Type is what the state means, not what it is called.
type LinearWorkflowState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Position float64 `json:"position"`
}

// LinearMilestone is a phase of a project, as M1 Foundation is a phase
// of Rig.
type LinearMilestone struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
}

// LinearProject is a project of the workspace.
type LinearProject struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Progress   float64           `json:"progress"`
	Milestones []LinearMilestone `json:"milestones,omitempty"`
}

// LinearRelation is a relation between two issues.
type LinearRelation struct {
	Type       string `json:"type"` // blocks, blockedBy, related, duplicate
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	State      string `json:"state"`
}

// LinearIssue is an issue, as the board shows it.
type LinearIssue struct {
	ID         string           `json:"id"`
	Identifier string           `json:"identifier"`
	Title      string           `json:"title"`
	URL        string           `json:"url,omitempty"`
	State      string           `json:"state"`
	StateType  string           `json:"stateType,omitempty"`
	Project    string           `json:"project,omitempty"`
	Milestone  string           `json:"milestone,omitempty"`
	Labels     []string         `json:"labels,omitempty"`
	Relations  []LinearRelation `json:"relations,omitempty"`
	// Blocked and Blocking count the relations of each way, which the
	// card shows as two numbers.
	Blocked  int `json:"blocked,omitempty"`
	Blocking int `json:"blocking,omitempty"`
	// Children is how many sub-issues the issue has.
	Children int `json:"children,omitempty"`
	// Session is the id of the session working on the issue, when one
	// is: the task and the session are linked.
	Session string `json:"session,omitempty"`
	// SessionName is that session's name, and ResumeID the agent's own
	// session, which resumes it.
	SessionName   string `json:"sessionName,omitempty"`
	ResumeID      string `json:"resumeId,omitempty"`
	SessionStatus Status `json:"sessionStatus,omitempty"`
	// Progress is what a program reported of the work, 0-100, or -1.
	Progress  int   `json:"progress,omitempty"`
	CreatedAt int64 `json:"createdAt,omitempty"`
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

// LinearAttachment is a file or a link on an issue.
type LinearAttachment struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Subtitle string `json:"subtitle,omitempty"`
}

// LinearComment is a comment on an issue: what an agent wrote when it
// finished, or what a person wrote.
type LinearComment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt int64  `json:"createdAt,omitempty"`
}

// LinearIssueDetail is one issue, whole: its description, the files on
// it, and the comments under it.
type LinearIssueDetail struct {
	LinearIssue
	Description string             `json:"description,omitempty"`
	BranchName  string             `json:"branchName,omitempty"`
	Assignee    string             `json:"assignee,omitempty"`
	Creator     string             `json:"creator,omitempty"`
	Attachments []LinearAttachment `json:"attachments,omitempty"`
	Comments    []LinearComment    `json:"comments,omitempty"`
	Parent      string             `json:"parent,omitempty"`
	SubIssues   []LinearIssue      `json:"subIssues,omitempty"`
}

// Project groups sessions, usually those of one repository.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dir  string `json:"dir,omitempty"`
}

// Pane is a terminal the daemon keeps running: a shell in a
// pseudo-terminal, one of a session's.
type Pane struct {
	ID      string   `json:"id"`
	Title   string   `json:"title,omitempty"` // set by the program (OSC 0/2)
	Dir     string   `json:"dir,omitempty"`   // the shell's working directory
	Command []string `json:"command,omitempty"`
	// Process is the foreground program, "" while the shell is.
	Process string `json:"process,omitempty"`
	// ProcessDir is the foreground program's working directory, as a
	// worktree a coding agent went into, while it is not the shell's.
	ProcessDir string `json:"processDir,omitempty"`
	// Agent names the coding agent running in the foreground, when known.
	Agent string `json:"agent,omitempty"`
	// Shell is the name of the pane's shell, as "fish".
	Shell string `json:"shell,omitempty"`

	Status Status `json:"status"`
	// Unread: something happened that the user has not seen: the work
	// ended, the program asked something, or the shell exited.
	Unread bool `json:"unread,omitempty"`
	// Message is the last notification's text, while Waiting, or what a
	// program reported with its status.
	Message string `json:"message,omitempty"`
	// Kind is what a program waits for (OSC 7501): permission, question
	// or auth.
	Kind string `json:"kind,omitempty"`
	// Progress is a program's progress, 0–100, or -1 when unknown.
	Progress int `json:"progress"`
	// Records are the status reports of the programs (OSC 7501), when
	// they sent some, by id.
	Records []Record `json:"records,omitempty"`
	// Since is when the status last changed (unix ms).
	Since int64 `json:"since"`
	// Activity is when the program last wrote (unix ms).
	Activity int64 `json:"activity"`
	Created  int64 `json:"created"`
	ExitCode int   `json:"exitCode,omitempty"`
	// CommandExit is the exit status of the command the pane started
	// with, once it ended and the pane went on with its shell.
	CommandExit *int `json:"commandExit,omitempty"`
	// AgentSession is the coding agent's own session, as its hook told
	// (kero hook), while the agent runs: what a restore resumes.
	AgentSession *AgentSession `json:"agentSession,omitempty"`
	// TaskID is the Linear issue the work is on, as "BRU-25": what
	// links a session to a task.
	TaskID string `json:"taskId,omitempty"`
	// Restored: the daemon restarted (as after a reboot) and this pane's
	// shell was started again in its directory, under its old scrollback.
	Restored bool `json:"restored,omitempty"`
	Cols     int  `json:"cols"`
	Rows     int  `json:"rows"`
	PID      int  `json:"pid,omitempty"`
}

// AgentSession is a coding agent's session: Claude Code's or Codex's,
// by its id.
type AgentSession struct {
	Agent string `json:"agent"` // "claude", "codex", "pi" or "opencode"
	ID    string `json:"id"`
}

// Session is a row of the sidebar: one task, with its terminals, its
// panes, side by side. Its Pane fields are those of its lead pane, the
// one that most needs the user (see Lead), so that a session reads as one.
type Session struct {
	Pane
	ID      string `json:"id"` // the session's, not the lead pane's
	Project string `json:"project"`
	Name    string `json:"name,omitempty"` // set by the user, or an agent
	// AgentNamed tells a Name an agent gave, for its conversation: the
	// next conversation in the session's first pane names it anew.
	AgentNamed bool `json:"agentNamed,omitempty"`
	// Panes are the session's terminals; Layout arranges them, nil for
	// the first alone.
	Panes  []Pane  `json:"panes"`
	Layout *Layout `json:"layout,omitempty"`
	// Lead is the pane the session's status comes from; Focus the pane
	// the user was in last; Zoomed the pane that fills the session, or "".
	Lead   string `json:"lead,omitempty"`
	Focus  string `json:"focus,omitempty"`
	Zoomed string `json:"zoomed,omitempty"`
}

// PaneByID returns a pane of the session, or nil.
func (s *Session) PaneByID(id string) *Pane {
	for i := range s.Panes {
		if s.Panes[i].ID == id {
			return &s.Panes[i]
		}
	}
	return nil
}

// FocusPane returns the pane the user was in last, else the first.
func (s *Session) FocusPane() *Pane {
	if p := s.PaneByID(s.Focus); p != nil {
		return p
	}
	if len(s.Panes) > 0 {
		return &s.Panes[0]
	}
	return nil
}

// urgency orders statuses by how much they need the user.
func urgency(p *Pane) int {
	switch {
	case p.Status == Waiting:
		return 5
	case p.Status == Failed:
		return 4
	case p.Status == Exited && p.Unread:
		return 3
	case p.Unread:
		return 2
	case p.Status == Working:
		return 1
	}
	return 0
}

// Lead returns the pane a session's status comes from: the one that most
// needs the user, the focused one among equals.
func Lead(panes []Pane, focus string) int {
	best := -1
	for i := range panes {
		if best < 0 || urgency(&panes[i]) > urgency(&panes[best]) ||
			urgency(&panes[i]) == urgency(&panes[best]) && panes[i].ID == focus {
			best = i
		}
	}
	return best
}

// PaneName is the name to show for a pane: the title a program in the
// foreground set, else the program, else the shell, else its directory's
// name.
func (p *Pane) PaneName() string {
	if p.Process != "" {
		if t := CleanTitle(p.Title); t != "" && !looksLikePath(t) {
			return t
		}
		return p.Process
	}
	if p.Shell != "" {
		return p.Shell
	}
	if p.Dir != "" {
		if p.Dir == Home() {
			return "~"
		}
		return filepath.Base(p.Dir)
	}
	return "shell"
}

// WorkDir is where the pane works: the foreground program's directory,
// else the shell's.
func (p *Pane) WorkDir() string {
	if p.ProcessDir != "" {
		return p.ProcessDir
	}
	return p.Dir
}

// DisplayName is the name to show for a session: the user's or an agent's, else the
// title a program in the foreground set, else its directory's name.
func (s *Session) DisplayName() string {
	if s.Name != "" {
		return s.Name
	}
	if s.Process != "" {
		if t := CleanTitle(s.Title); t != "" && !looksLikePath(t) {
			return t
		}
		if s.Agent != "" {
			return s.Agent
		}
	}
	if s.Dir != "" {
		if s.Dir == Home() {
			return "~"
		}
		return filepath.Base(s.Dir)
	}
	return "shell"
}

// CleanTitle strips the status glyphs agents put before their titles:
// spinners (braille, stars) and bullets.
func CleanTitle(t string) string {
	t = strings.TrimSpace(t)
	for {
		r, size := firstRune(t)
		if size == 0 || !isTitleGlyph(r) {
			break
		}
		t = strings.TrimSpace(t[size:])
	}
	return t
}

// TitleBusy reports whether a title shows a spinner, as agents' titles do
// while they work: a braille pattern, or a spinner's star.
func TitleBusy(t string) bool {
	r, _ := firstRune(strings.TrimSpace(t))
	return r > 0x2800 && r <= 0x28FF || strings.ContainsRune("◐◓◑◒", r)
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

func isTitleGlyph(r rune) bool {
	switch {
	case r >= 0x2800 && r <= 0x28FF: // braille spinners
		return true
	case r >= 0x2700 && r <= 0x27BF: // dingbats: ✳ ✶ ✻ ✽ ✓ ✗
		return true
	case r >= 0x25A0 && r <= 0x25FF: // geometric shapes: ◇ ◆ ● ◐
		return true
	case r >= 0x2600 && r <= 0x26FF: // misc symbols: ⚡ ☐
		return true
	case strings.ContainsRune("·•*✦✧⏺⏵>»", r):
		return true
	}
	return false
}

func looksLikePath(t string) bool {
	return strings.HasPrefix(t, "/") || strings.HasPrefix(t, "~") || strings.Contains(t, "@") && strings.Contains(t, ":") ||
		windowsPath(t) || consoleTitle(t)
}

// windowsPath tells a path of a drive: C:\Users.
func windowsPath(t string) bool {
	return len(t) > 2 && t[1] == ':' && (t[2] == '\\' || t[2] == '/')
}

// consoleTitle tells the title Windows gives a console, its program's
// path, after "Administrator: " in the user's language when it is
// elevated, and before " - ping" while cmd.exe runs a command:
// "管理员: C:\Program Files\PowerShell\7\pwsh.exe".
func consoleTitle(t string) bool {
	i := strings.Index(t, `:\`)
	if i < 1 || !windowsPath(t[i-1:]) {
		return false
	}
	if prefix := strings.TrimSpace(t[:i-1]); prefix != "" && !strings.HasSuffix(prefix, ":") {
		return false
	}
	exe, _, _ := strings.Cut(t[i-1:], " - ")
	return strings.HasSuffix(strings.ToLower(exe), ".exe")
}

// ShortDir writes a directory with ~ for the home directory.
func ShortDir(dir string) string {
	home := Home()
	if home != "" && Within(dir, home) {
		return "~" + dir[len(home):]
	}
	return dir
}

// Within reports whether path is dir or in it. Windows' paths are of any
// case.
func Within(path, dir string) bool {
	if len(path) < len(dir) || dir == "" {
		return false
	}
	head := path[:len(dir)]
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(head, dir) {
			return false
		}
	} else if head != dir {
		return false
	}
	rest := path[len(dir):]
	return rest == "" || os.IsPathSeparator(rest[0]) || os.IsPathSeparator(dir[len(dir)-1])
}

// Home returns the user's home directory.
func Home() string {
	h, _ := os.UserHomeDir()
	return h
}

// Name names kero's directories: "kero", or "kero-dev" for a development
// build, so that it runs a daemon, sessions and settings of its own beside
// the installed Kero.
func Name() string {
	// Keep the personal edition's storage identity stable across its rename.
	if strings.HasPrefix(mygo.App.Name(), "Kero Local") || strings.HasPrefix(mygo.App.Name(), "Shibu Code") {
		if mygo.IsDev() {
			return "kero-local-dev"
		}
		return "kero-local"
	}
	if mygo.IsDev() {
		return "kero-dev"
	}
	return "kero"
}

// Dir returns kero's directory: $KERO_HOME, else
// ~/Library/Application Support/kero (~/.config/kero on Linux,
// %AppData%\kero on Windows), of Name. The app, the daemon and the CLI of
// a build share it.
func Dir() string {
	if d := os.Getenv("KERO_HOME"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(Home(), ".config")
	}
	return filepath.Join(base, Name())
}

// SocketPath returns the daemon's socket: $KERO_SOCKET, else kerod.sock in
// Dir.
func SocketPath() string {
	if s := os.Getenv("KERO_SOCKET"); s != "" {
		return s
	}
	p := filepath.Join(Dir(), "kerod.sock")
	if len(p) > 100 {
		// Past what a Unix socket's address holds (104 bytes on macOS):
		// a short path of the user's, named after the directory.
		h := fnv.New32a()
		h.Write([]byte(Dir()))
		if runtime.GOOS == "windows" {
			// Its temporary directory is the user's.
			return filepath.Join(os.TempDir(), fmt.Sprintf("kero-%08x.sock", h.Sum32()))
		}
		p = fmt.Sprintf("/tmp/kero-%d-%08x.sock", os.Getuid(), h.Sum32())
	}
	return p
}

// StopEvent names the event that asks the daemon of a pid to stop on
// Windows, where it has no signals.
func StopEvent(pid int) string { return fmt.Sprintf(`Local\kerod-stop-%d`, pid) }

// Ago writes how long ago a time was, as the sidebar shows it: "now",
// "4m", "2h", "3d".
func Ago(ms int64, now time.Time) string {
	if ms == 0 {
		return ""
	}
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	case d < 7*24*time.Hour:
		return itoa(int(d/(24*time.Hour))) + "d"
	}
	return itoa(int(d/(7*24*time.Hour))) + "w"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Params of the control methods.
type (
	CreateParams struct {
		// Split, a session or a pane, makes a pane beside that pane (the
		// session's focused one) instead of a session; Direction is
		// where: right (the default), down, left or up, and Ratio the
		// share of the new pane, 0.5 when zero.
		Split     string  `json:"split,omitempty"`
		Direction string  `json:"direction,omitempty"`
		Ratio     float64 `json:"ratio,omitempty"`

		Project string   `json:"project,omitempty"` // id or name; "" the first
		Name    string   `json:"name,omitempty"`
		Dir     string   `json:"dir,omitempty"` // "" the project's, else home
		Command []string `json:"command,omitempty"`
		Env     []string `json:"env,omitempty"`
		After   string   `json:"after,omitempty"` // put it after this session
		Cols    int      `json:"cols,omitempty"`
		Rows    int      `json:"rows,omitempty"`
	}
	IDParams struct {
		ID string `json:"id"`
	}
	RenameParams struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		// By is the pane the rename comes from, when one does: a name
		// from a pane running an agent is the agent's.
		By string `json:"by,omitempty"`
	}
	MoveParams struct {
		ID      string `json:"id"`
		Project string `json:"project,omitempty"`
		Index   int    `json:"index"` // within the project; -1 last
	}
	SendParams struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}
	ReadParams struct {
		ID         string `json:"id"`
		Scrollback bool   `json:"scrollback,omitempty"`
		Lines      int    `json:"lines,omitempty"`
	}
	ViewParams struct {
		ID string `json:"id"` // "" when no window shows a session
	}
	DoParams struct {
		Action string            `json:"action"`
		Args   map[string]string `json:"args,omitempty"`
	}
	FocusParams struct {
		Session string `json:"session"`
		Pane    string `json:"pane"`
	}
	LayoutParams struct {
		Session string  `json:"session"`
		Layout  *Layout `json:"layout"`
	}
	NotifyParams struct {
		ID    string `json:"id"`
		Title string `json:"title,omitempty"`
		Body  string `json:"body,omitempty"`
	}
	WaitParams struct {
		ID string `json:"id"`
		// Status waits for one of these; empty waits for the session to
		// stop working: idle, waiting or exited.
		Status []Status `json:"status,omitempty"`
		// Exit waits instead for the command the pane started with to
		// end (CommandExit), or for its shell to.
		Exit    bool  `json:"exit,omitempty"`
		Timeout int64 `json:"timeout,omitempty"` // ms; 0 waits as long as it takes
	}
	// AgentSessionParams tell a pane's agent's session (kero hook).
	// The pane is the one whose shell runs PID, the hook's process, else
	// the one that can only be the agent's: an agent's service that panes
	// share, as Codex's app server, runs hooks under none.
	AgentSessionParams struct {
		PID     int    `json:"pid"`
		Agent   string `json:"agent"`
		Session string `json:"session"`
		Dir     string `json:"dir,omitempty"` // the agent's, to tell its pane by
	}
	// AgentSessionResult is the pane told of, and its session; Away: the
	// agent runs under none of kero's shells, and so do its commands.
	AgentSessionResult struct {
		Pane    string `json:"pane"`
		Session string `json:"session"`
		Away    bool   `json:"away,omitempty"`
	}
	ProjectParams struct {
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
		Dir  string `json:"dir,omitempty"`
	}
	MoveProjectParams struct {
		ID    string `json:"id"`
		Index int    `json:"index"`
	}
	ReadResult struct {
		Text string `json:"text"`
	}
	// LinkParams binds a session to a Linear issue, or unbinds it.
	LinkParams struct {
		Session string `json:"session"`
		Task    string `json:"task,omitempty"`
	}
	// LinearParams asks about a workspace, or about one issue of it.
	LinearParams struct {
		Issue string `json:"issue,omitempty"`
		State string `json:"state,omitempty"`
		Body  string `json:"body,omitempty"`
	}
)
