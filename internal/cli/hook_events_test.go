package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kero/internal/proto"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func hookIn(t *testing.T, raw string) hookInput {
	t.Helper()
	var in hookInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	return in
}

// Each event an agent's hook gets tells its pane's status as the table
// of docs/status.md has it, or nothing.
func TestEventReport(t *testing.T) {
	inPane := env(map[string]string{"KERO_PANE": "p1"})
	for _, c := range []struct {
		agent, in        string
		state, kind, msg string // "" state: no report
	}{
		{"claude", `{"hook_event_name":"UserPromptSubmit","session_id":"s","prompt":"hi"}`, "working", "", ""},
		{"claude", `{"hook_event_name":"Notification","session_id":"s","notification_type":"permission_prompt","message":"Claude needs your permission to use Bash"}`,
			"blocked", "permission", "Claude needs your permission to use Bash"},
		{"claude", `{"hook_event_name":"Notification","session_id":"s","notification_type":"elicitation_dialog","message":"Pick one"}`, "blocked", "permission", "Pick one"},
		{"claude", `{"hook_event_name":"Notification","session_id":"s","notification_type":"idle_prompt","message":"Claude is waiting for your input"}`, "", "", ""},
		{"claude", `{"hook_event_name":"PostToolUse","session_id":"s","tool_name":"Bash","tool_input":{"command":"ls"}}`, "working", "", ""},
		{"claude", `{"hook_event_name":"Stop","session_id":"s","stop_hook_active":false,"last_assistant_message":"Done."}`, "done", "", ""},
		{"claude", `{"hook_event_name":"StopFailure","session_id":"s","error":"rate_limit","error_details":"429 Too Many Requests","last_assistant_message":"API Error: Rate limit reached"}`,
			"error", "", "API Error: Rate limit reached"},
		{"claude", `{"hook_event_name":"StopFailure","session_id":"s","error":"server_error"}`, "error", "", "server_error"},
		{"claude", `{"hook_event_name":"SessionEnd","session_id":"s","reason":"clear"}`, "clear", "", ""},
		// A subagent's end is not the turn's.
		{"claude", `{"hook_event_name":"SubagentStop","session_id":"s","agent_id":"a1"}`, "", "", ""},
		{"claude", `{"hook_event_name":"SubagentStart","session_id":"s","agent_id":"a1"}`, "", "", ""},
		{"claude", `{"hook_event_name":"Stop","session_id":"s","agent_id":"a1"}`, "", "", ""},
		{"claude", `{"hook_event_name":"StopFailure","session_id":"s","agent_id":"a1","error":"unknown"}`, "", "", ""},
		// Events kero doesn't hook, and Codex's, tell nothing of Claude.
		{"claude", `{"hook_event_name":"PreToolUse","session_id":"s"}`, "", "", ""},
		{"claude", `{"hook_event_name":"PermissionRequest","session_id":"s"}`, "", "", ""},
		{"claude", `{"hook_event_name":"SomethingNew","session_id":"s"}`, "", "", ""},
		{"claude", `{}`, "", "", ""},

		{"codex", `{"hook_event_name":"UserPromptSubmit","session_id":"s","turn_id":"t"}`, "working", "", ""},
		{"codex", `{"hook_event_name":"PermissionRequest","session_id":"s","tool_name":"Bash","tool_input":{"command":"rm -rf build","description":"Remove the build"}}`,
			"blocked", "permission", "Remove the build"},
		{"codex", `{"hook_event_name":"PermissionRequest","session_id":"s","tool_name":"Bash","tool_input":{"command":"rm -rf build","description":null}}`,
			"blocked", "permission", "rm -rf build"},
		{"codex", `{"hook_event_name":"PostToolUse","session_id":"s","tool_response":{"ok":true}}`, "working", "", ""},
		{"codex", `{"hook_event_name":"Stop","session_id":"s","last_assistant_message":null}`, "done", "", ""},
		{"codex", `{"hook_event_name":"Notification","session_id":"s","notification_type":"permission_prompt"}`, "", "", ""},
		{"codex", `{"hook_event_name":"SubagentStop","session_id":"s","agent_id":"a"}`, "", "", ""},
		{"codex", `{"hook_event_name":"Interrupt","session_id":"s"}`, "", "", ""},
	} {
		p, ok := eventReport(c.agent, hookIn(t, c.in), inPane)
		if !ok {
			if c.state != "" {
				t.Errorf("%s %s: no report, want %s", c.agent, c.in, c.state)
			}
			continue
		}
		want := proto.ReportParams{Pane: "p1", Agent: c.agent, Session: "s", State: c.state, Kind: c.kind, Msg: c.msg}
		if p != want {
			t.Errorf("%s %s:\n got %+v\nwant %+v", c.agent, c.in, p, want)
		}
	}
}

// A hook that can't tell that its agent is its pane's says nothing: out
// of Kero, of no session, or of a Codex that another agent in the pane
// runs.
func TestEventReportOnlyForItsPane(t *testing.T) {
	prompt := hookIn(t, `{"hook_event_name":"UserPromptSubmit","session_id":"s"}`)
	if _, ok := eventReport("claude", prompt, env(nil)); ok {
		t.Error("reported without KERO_PANE")
	}
	if _, ok := eventReport("claude", hookIn(t, `{"hook_event_name":"UserPromptSubmit"}`), env(map[string]string{"KERO_PANE": "p1"})); ok {
		t.Error("reported without a session")
	}
	other := env(map[string]string{"KERO_PANE": "p1", "CODEX_THREAD_ID": "the-panes"})
	if _, ok := eventReport("codex", prompt, other); ok {
		t.Error("a Codex of another thread reported")
	}
	same := env(map[string]string{"KERO_PANE": "p1", "CODEX_THREAD_ID": "s"})
	if _, ok := eventReport("codex", prompt, same); !ok {
		t.Error("the pane's Codex did not report")
	}
}

// A status event says nothing to the agent, on stdout, whatever kerod
// does: Claude Code would take it into its context.
func TestHookStatusEventsSayNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KERO_HOME", filepath.Join(dir, "home"))
	t.Setenv("KERO_CONFIG", filepath.Join(dir, "config"))
	t.Setenv("KERO_SOCKET", filepath.Join(dir, "no.sock")) // no kerod, and none starts
	t.Setenv("KERO_PANE", "p1")
	t.Setenv("KERO_SESSION", "")
	t.Setenv("CODEX_THREAD_ID", "")
	for _, in := range []string{
		`{"hook_event_name":"UserPromptSubmit","session_id":"s","prompt":"hi"}`,
		`{"hook_event_name":"Notification","session_id":"s","notification_type":"permission_prompt","message":"m"}`,
		`{"hook_event_name":"Stop","session_id":"s"}`,
		`{"hook_event_name":"SessionEnd","session_id":"s"}`,
		`{"hook_event_name":"SomethingNew","session_id":"s"}`,
		`not json`,
	} {
		if got := runHook(t, "claude", in); got != "" {
			t.Errorf("%s: said %q", in, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "home")); err == nil {
		t.Error("the hook started kerod")
	}
}

// runHook runs `kero hook <agent>` with in on stdin, returning its stdout.
func runHook(t *testing.T, agent, in string) string {
	t.Helper()
	stdin, stdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = stdin, stdout }()
	f := filepath.Join(t.TempDir(), "in.json")
	os.WriteFile(f, []byte(in), 0o600)
	inf, err := os.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	defer inf.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = inf, w
	if err := hookCmd([]string{agent}); err != nil {
		t.Errorf("%s: %v", in, err)
	}
	w.Close()
	var out bytes.Buffer
	io.Copy(&out, r)
	return out.String()
}

// An install of before, of SessionStart alone, is outdated; setup adds
// the other events and leaves SessionStart's group as it was, where it
// was, which Codex trusts it by.
func TestHookUpgradeKeepsSessionStart(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			h := testHook(t, "")
			h.agent = agent
			start := `[{"matcher": "startup", "hooks": [{"type": "command", "command": "echo hi"}]}, {"hooks": [{"type": "command", "command": ` +
				string(marshal(h.command())) + `, "timeout": 10}]}]`
			old := `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "say done"}]}], "SessionStart": ` + start + `}}`
			if err := os.WriteFile(h.file, []byte(old), 0o600); err != nil {
				t.Fatal(err)
			}
			if st := h.state(); st != "outdated" {
				t.Fatalf("SessionStart alone: %s", st)
			}
			if err := h.install(); err != nil {
				t.Fatal(err)
			}
			if st := h.state(); st != "current" {
				t.Fatalf("after install: %s", st)
			}
			settings, _ := parseObject([]byte(read(t, h.file)))
			hooks, _ := parseObject(settings.get("hooks"))
			var was, is bytes.Buffer
			json.Compact(&was, []byte(start))
			json.Compact(&is, hooks.get("SessionStart"))
			if was.String() != is.String() {
				t.Fatalf("SessionStart changed:\n%s\n%s", was.String(), is.String())
			}
			if hooks[0].Key != "Stop" || hooks[1].Key != "SessionStart" {
				t.Fatalf("events moved: %v", hooks)
			}
			for _, ev := range hookEvents[agent] {
				n := 0
				h.each(settings, func(event, command string) {
					if event == ev.name && command == h.command() {
						n++
					}
				})
				if n != 1 {
					t.Errorf("%s: %d of kero's hooks", ev.name, n)
				}
			}
			stop := string(hooks.get("Stop"))
			if !strings.Contains(stop, "say done") || !strings.Contains(stop, "hook "+agent) {
				t.Fatalf("Stop: %s", stop)
			}
			// Again changes nothing; removing leaves only the user's.
			before := read(t, h.file)
			if err := h.install(); err != nil {
				t.Fatal(err)
			}
			if read(t, h.file) != before {
				t.Fatal("installing again changed it")
			}
			if err := h.remove(); err != nil {
				t.Fatal(err)
			}
			got := read(t, h.file)
			if strings.Contains(got, "kero") || !strings.Contains(got, "echo hi") || !strings.Contains(got, "say done") {
				t.Fatalf("after remove:\n%s", got)
			}
			if st := h.state(); st != "missing" {
				t.Fatalf("after remove: %s", st)
			}
		})
	}
}
