package settings

// ScrollbackBytes is the scrollback a terminal keeps, as terminal.Options
// takes it: the setting's megabytes, or -1 for none. The daemon's
// terminals and the app's both take it, so that one setting bounds the
// two copies of a session's history.
func (s Settings) ScrollbackBytes() int {
	if s.Scrollback <= 0 {
		return -1
	}
	return s.Scrollback << 20
}
