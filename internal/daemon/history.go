package daemon

import "bytes"

// historyTailBytes bounds what the periodic save writes of a pane's
// history. The whole scrollback goes to disk only as the daemon ends
// (saveHistories(true)); a daemon that dies without ending, as when the
// Mac loses power, restores this much of each pane, which still holds
// its last screen.
const historyTailBytes = 256 << 10

// historyTail returns the last rows of a history (terminal.History) that
// fit in max bytes. It cuts only where a row ended, after its "\r\n", so
// that each row it keeps starts as it did: History ends every row with
// its colors reset, and a soft-wrapped row runs on to its next without a
// break. A history with no such place in its last max bytes, one long
// wrapped line, is kept whole.
func historyTail(h []byte, max int) []byte {
	if len(h) <= max {
		return h
	}
	from := len(h) - max
	i := bytes.Index(h[from:], []byte("\r\n"))
	if i < 0 || from+i+2 >= len(h) {
		return h
	}
	return h[from+i+2:]
}
