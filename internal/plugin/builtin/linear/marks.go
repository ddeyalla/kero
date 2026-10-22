package linear

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
	"kero/locales"
)

// The marks of the pane: the icon and the color of a state, a priority and a
// session, drawn with the icons and the color roles the protocol has. The
// team's own colors are not used: a plugin draws with the app's roles.

// stateIcon and stateColor draw a workflow state by what it means, as
// Linear does: a dashed ring for the backlog, a ring to start, a half-filled
// ring for work under way, a check for done and a cross for dropped.
func stateIcon(stateType string) string {
	switch sectionOf(stateType) {
	case secBacklog:
		return "circle-dashed"
	case secCurrent:
		return "circle-half"
	case secDone:
		return "check-circle"
	case secCanceled:
		return "xmark-circle"
	}
	return "circle"
}

func stateColor(stateType string) string {
	switch sectionOf(stateType) {
	case secCurrent:
		return protocol.ColorWarning
	case secDone:
		return protocol.ColorPurple
	}
	return protocol.ColorMuted
}

// readableHex tells whether a team's color for a state can be drawn as it is:
// a color, and neither so light nor so dark that it all but vanishes on the
// light or on the dark theme, as Linear's pale greys for Todo and Backlog do
// on white. A color that fails is left to the role color, which the app
// picks for the theme.
func readableHex(hex string) bool {
	if !protocol.ValidHex(hex) {
		return false
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#")[:6], 16, 32)
	if err != nil {
		return false
	}
	lin := func(c uint64) float64 {
		f := float64(c) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(v>>16&0xff) + 0.7152*lin(v>>8&0xff) + 0.0722*lin(v&0xff)
	// At most half as bright as white's contrast needs, and at least twice
	// as bright as the dark theme's background.
	return l >= 0.06 && l <= 0.5
}

// sectionTitle is a section's name.
func sectionTitle(key string) string {
	switch key {
	case secCurrent:
		return locales.LinearSectionCurrent()
	case secDone:
		return locales.LinearSectionDone()
	case secTodo:
		return locales.LinearSectionTodo()
	case secBacklog:
		return locales.LinearSectionBacklog()
	}
	return locales.LinearSectionCanceled()
}

// sectionStateType is a state type that stands for a section, for its icon.
func sectionStateType(key string) string {
	switch key {
	case secCurrent:
		return "started"
	case secDone:
		return "completed"
	case secBacklog:
		return "backlog"
	case secCanceled:
		return "canceled"
	}
	return "unstarted"
}

// priorityMark is the icon and the color of an issue's priority, none for
// an issue without one.
func priorityMark(priority int) (icon, color string) {
	icon = "priority-" + priorityKey(priority)
	if priority < 1 || priority > 4 || !slices.Contains(protocol.KnownIcons, icon) {
		if priority < 1 || priority > 4 {
			return "", ""
		}
		icon = "flag"
	}
	switch priority {
	case 1:
		return icon, protocol.ColorDanger
	case 2:
		return icon, protocol.ColorOrange
	case 3:
		return icon, protocol.ColorWarning
	}
	return icon, protocol.ColorMuted
}

func priorityKey(priority int) string {
	switch priority {
	case 1:
		return "urgent"
	case 2:
		return "high"
	case 3:
		return "medium"
	case 4:
		return "low"
	}
	return "none"
}

// priorityName is a priority's name in words.
func priorityName(priority int) string {
	switch priority {
	case 1:
		return locales.LinearPriorityUrgent()
	case 2:
		return locales.LinearPriorityHigh()
	case 3:
		return locales.LinearPriorityMedium()
	case 4:
		return locales.LinearPriorityLow()
	}
	return locales.LinearPriorityNone()
}

// sessionColor is the color of the badge of the session working on an
// issue: by what it needs of the user.
func sessionColor(st proto.Status) string {
	switch st {
	case proto.Working:
		return protocol.ColorAccent
	case proto.Waiting:
		return protocol.ColorWarning
	case proto.Failed:
		return protocol.ColorDanger
	case proto.Idle:
		return protocol.ColorSuccess
	}
	return protocol.ColorMuted
}

// agentTitle is a harness's name, as its makers write it.
func agentTitle(agent string) string {
	switch agent {
	case "":
		return locales.LinearNoAgent()
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "pi":
		return "Pi"
	case "gemini":
		return "Gemini CLI"
	case "amp":
		return "Amp"
	case "cursor-agent":
		return "Cursor"
	case "copilot":
		return "Copilot"
	}
	return strings.ToUpper(agent[:1]) + agent[1:]
}

// viaWord says how a task was linked, when not by a person.
func viaWord(via string) string {
	switch via {
	case "task":
		return locales.LinearViaAgent()
	case "branch":
		return locales.LinearViaBranch()
	}
	return ""
}

// relationWord is what a relation does, in a word or two.
func relationWord(typ string) string {
	switch typ {
	case "blocks":
		return locales.LinearRelationBlocks()
	case "blockedBy":
		return locales.LinearRelationBlockedBy()
	case "duplicate":
		return locales.LinearRelationDuplicate()
	}
	return locales.LinearRelationRelated()
}

// markdown is Linear's markdown: a line break in it is a break, as it is in
// Linear's editor, and not the soft wrap markdown reads it as. The ends of
// a paragraph's lines get the backslash that makes a hard break; those
// inside a code block are left alone.
func markdown(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	fence := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			fence = !fence
		}
		if !fence && i < len(lines)-1 {
			next := strings.TrimSpace(lines[i+1])
			switch {
			case l == "" || next == "":
			case isBlock(next):
			case strings.HasPrefix(trimmed, "#") && strings.HasPrefix(strings.TrimLeft(trimmed, "#"), " "):
				// A heading ends where its line does.
			case strings.HasSuffix(l, "\\"):
				// The line already breaks.
			default:
				l = strings.TrimRight(l, " \t") + "\\"
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// isBlock says whether a line starts a block of its own, such as a list
// item, a heading or a quote, which a line before it does not break on. A
// bold word is not a list: "**Design:**" is a list only when a space
// follows the star.
func isBlock(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '-', '*', '+', '#':
		return len(line) > 1 && line[1] == ' '
	case '>':
		return true
	}
	return false
}

// clip cuts a markdown source to the protocol's limit on a line end.
func clip(src string) string {
	if len(src) <= protocol.MaxMarkdown {
		return src
	}
	cut := src[:protocol.MaxMarkdown]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return strings.ToValidUTF8(cut, "")
}

// itoa is a number as words.
func itoa(n int) string { return strconv.Itoa(n) }
