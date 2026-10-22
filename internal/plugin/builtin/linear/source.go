package linear

// newDaemon is the daemon the plugin talks to. It is one small function so
// that a test can replace this file through the go tool's overlay and run
// the real app as the plugin against a daemon of its own.
func newDaemon() Daemon { return &liveDaemon{} }
