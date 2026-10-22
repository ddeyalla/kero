package settings

// ImageLimitBytes is the most a terminal's Kitty graphics images keep, as
// terminal.Options takes it: 32 MB, against MyGo's 320 MB, for each of the
// two copies a pane has.
const ImageLimitBytes = 32 << 20

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
