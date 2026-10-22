package main

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// A session's title can carry a mark at its head that says what the work
// is doing: an SF Symbol the app derives, never one the user types, and
// never part of the name kerod keeps. It follows the session's status and
// the type of the Linear task it is linked to, from the state the app has
// already: no field of the protocol holds it.
//
// The set is closed, and small:
//
//	exclamationmark.circle  the program waits on the user
//	ladybug                 the linked task is a bug
//	hourglass               the agent or the program is at work
//	checkmark.circle        the work ended, and the user has seen it
//
// Other sessions have no mark: a shell at rest, a turn ended unseen (its
// bold title and filled check say so), a failure or an exit (the status
// glyph at the right says so in red).

// The marks a title can carry.
const (
	markNeedsYou = "exclamationmark.circle"
	markBug      = "ladybug"
	markWorking  = "hourglass"
	markDone     = "checkmark.circle"
)

// titleMarkSize is the side of a title's mark, in points: a little under
// the title's capital height plus its descender, so that it sits on the
// line with the text.
const titleMarkSize = 12

// titleMark is the mark at the head of a session's title, "" for none:
// what needs the user first, then the type of the task, then what the
// work does. lin is the Linear workspace the app last had, nil for none.
func titleMark(s *proto.Session, lin *proto.LinearState) string {
	switch {
	case s.Status == proto.Waiting:
		return markNeedsYou
	case bugTask(s.TaskID, lin):
		return markBug
	case s.Status == proto.Working:
		return markWorking
	case s.Status == proto.Idle && !s.Unread && turnEnded(s):
		return markDone
	}
	return ""
}

// turnEnded tells a session at rest whose work ended, rather than a shell
// that never started any: kerod's fold says "done" of one that a program
// reported on, and one whose status changed since its pane began has been
// at work and back, as an agent whose turn kerod saw on its screen (which
// leaves no words behind).
func turnEnded(s *proto.Session) bool {
	if s.Attention != nil && s.Attention.State == proto.AttentionDone {
		return true
	}
	return s.Message != "" || s.Created != 0 && s.Since > s.Created
}

// bugTask tells a Linear task that is a bug: an issue of the workspace by
// its identifier, labeled Bug, as Linear's own label for the type is.
// Linear has no type of issue but its labels.
func bugTask(id string, lin *proto.LinearState) bool {
	if id == "" || lin == nil {
		return false
	}
	for i := range lin.Issues {
		if !strings.EqualFold(lin.Issues[i].Identifier, id) {
			continue
		}
		for _, l := range lin.Issues[i].Labels {
			if strings.EqualFold(strings.TrimSpace(l), "bug") {
				return true
			}
		}
		return false
	}
	return false
}

// titleMarkLabel is what a mark says, to a screen reader and a test.
func titleMarkLabel(mark string) string {
	switch mark {
	case markNeedsYou:
		return locales.StatusNeedsYou()
	case markBug:
		return locales.StatusBugWork()
	case markWorking:
		return locales.StatusWorking()
	case markDone:
		return locales.StatusDone()
	}
	return ""
}

// titleMarkColor is a mark's tint: the amber of what needs the user, and
// the quiet text colors for the rest, so that the title stays what reads
// first.
func titleMarkColor(pal *palette, mark string) ui.Color {
	switch mark {
	case markNeedsYou:
		return pal.attention
	case markBug, markWorking:
		return pal.textMuted
	}
	return pal.textFaint
}

// titleMarkView draws a session's title mark in a slot of its own, which
// never shrinks: a long title gives way, the mark keeps its place. A
// session without a mark keeps the slot empty, so that every title in
// the sidebar starts at one edge.
func titleMarkView(c *ui.Context, pal *palette, mark string) {
	slot := ui.Box(c).Size(titleMarkSize, titleMarkSize).Shrink(0).Center()
	if mark == "" {
		return
	}
	slot.Label(titleMarkLabel(mark))
	b := symbolImage(mark, titleMarkSize, titleMarkColor(pal, mark), pal.dark)
	if b == nil {
		return
	}
	slot.Children(func() {
		ui.Image(c, b).Size(titleMarkSize, titleMarkSize)
	})
}
