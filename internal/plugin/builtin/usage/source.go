package usage

// newSource is the account usage and the machine sample a pane reads. It is
// one small function so that a test can replace this file through the go
// tool's overlay and run the real app as the plugin with a source of its own.
func newSource() Source { return &liveSource{} }
