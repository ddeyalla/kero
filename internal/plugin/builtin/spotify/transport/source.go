package transport

import "errors"

// ErrUnsupported is the error a Source returns on a platform without
// the distributed notification center.
var ErrUnsupported = errors.New("transport: the playback listener needs macOS")

// Source tells a Monitor that Spotify's playback state changed, so that
// the Monitor reads at once instead of waiting for its next poll.
//
// macOS delivers these changes as distributed notifications, which the
// plugin hears through the system's notification center. A Source may
// be missing or fail to start: the Monitor then polls, and still works.
type Source interface {
	// Start begins listening. handler is called once for each change,
	// on the goroutine that delivers notifications, and must not block:
	// a slow handler holds up every later notification.
	Start(handler func()) error
	// Close stops listening and releases everything Start registered.
	Close() error
}
