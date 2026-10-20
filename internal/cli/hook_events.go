package cli

import (
	"encoding/json"
	"time"

	"kero/internal/client"
	"kero/internal/proto"
)

// hookInput is what an agent gives its hooks on stdin, of the events kero
// hooks: Claude Code's and Codex's, which share their names and fields.
type hookInput struct {
	Event   string `json:"hook_event_name"`
	AgentID string `json:"agent_id"`   // set for Claude Code's subagents
	Session string `json:"session_id"` // the agent's own
	Cwd     string `json:"cwd"`
	Source  string `json:"source"` // "startup", "resume", "clear", "compact"
	// Notification: Claude Code's, as "permission_prompt", and its words.
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
	// StopFailure: the API's error, as "rate_limit", and its text.
	Error        json.RawMessage `json:"error"`
	ErrorDetails json.RawMessage `json:"error_details"`
	LastMessage  json.RawMessage `json:"last_assistant_message"`
	// PermissionRequest: Codex's tool, whose input may say why.
	ToolInput json.RawMessage `json:"tool_input"`
}

// hookEvent is an event kero hooks an agent's turns by, and how long the
// agent gives the hook: a moment, as it waits on it.
type hookEvent struct {
	name    string
	timeout int // seconds
}

// hookEvents are the events kero hooks in each agent, SessionStart first:
// it tells the agent of Kero, and the others its status (eventReport).
var hookEvents = map[string][]hookEvent{
	"claude": {
		{"SessionStart", 10}, {"UserPromptSubmit", 3}, {"Notification", 3}, {"PostToolUse", 3},
		{"Stop", 3}, {"StopFailure", 3}, {"SessionEnd", 2},
	},
	"codex": {
		{"SessionStart", 10}, {"UserPromptSubmit", 3}, {"PermissionRequest", 3}, {"PostToolUse", 3}, {"Stop", 3},
	},
}

// eventReport is the status an agent's hook event tells, for the pane the
// hook runs in: ok is false for an event that tells none, as a
// subagent's, and for a hook that can't tell that the agent is its
// pane's, out of Kero or as a Codex another agent in the pane runs.
// kerod tells the rest: the session must be the pane's (hookReport).
func eventReport(agent string, in hookInput, getenv func(string) string) (p proto.ReportParams, ok bool) {
	pane := getenv("KERO_PANE")
	if pane == "" || in.Session == "" {
		return p, false
	}
	if t := getenv("CODEX_THREAD_ID"); t != "" && t != in.Session {
		return p, false // a Codex run by another agent in this pane, not the pane's
	}
	p = proto.ReportParams{Pane: pane, Agent: agent, Session: in.Session}
	switch agent + " " + in.Event {
	case "claude UserPromptSubmit", "codex UserPromptSubmit",
		"claude PostToolUse", "codex PostToolUse": // a permission given, its tool ran
		p.State = "working"
	case "claude Notification":
		// Claude Code's other notifications, as that it waits for the
		// next prompt, tell no more than its Stop did.
		if in.NotificationType != "permission_prompt" && in.NotificationType != "elicitation_dialog" {
			return p, false
		}
		p.State, p.Kind, p.Msg = "blocked", "permission", in.Message
	case "codex PermissionRequest":
		p.State, p.Kind = "blocked", "permission"
		var tool struct {
			Description json.RawMessage `json:"description"`
			Command     json.RawMessage `json:"command"`
		}
		json.Unmarshal(in.ToolInput, &tool)
		p.Msg = firstString(tool.Description, tool.Command)
	case "claude Stop", "codex Stop":
		if in.AgentID != "" {
			return p, false
		}
		p.State = "done"
	case "claude StopFailure":
		if in.AgentID != "" {
			return p, false
		}
		p.State, p.Msg = "error", firstString(in.LastMessage, in.ErrorDetails, in.Error)
	case "claude SessionEnd":
		// Not done: Claude Code ends a session on /clear too, for the
		// next.
		if in.AgentID != "" {
			return p, false
		}
		p.State = "clear"
	default:
		return p, false
	}
	return p, true
}

// firstString is the first of JSON values that is a string with text.
func firstString(vals ...json.RawMessage) string {
	for _, v := range vals {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// sendReport tells kerod an agent's status, if it runs, and gives up
// after a moment: the agent waits on its hook, and a kerod that doesn't
// know the method, of before it, refuses it, which is as well.
func sendReport(p proto.ReportParams) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := client.Connect(true)
		if err != nil {
			return
		}
		defer c.Close()
		c.Call("report", p, nil)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
