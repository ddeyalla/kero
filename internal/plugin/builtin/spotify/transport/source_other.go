//go:build !darwin

package transport

// source is the Source of a build that cannot listen to Spotify. The
// Monitor then polls, which still works.
type source struct{}

// NewSource returns a Source that cannot listen. Start reports
// ErrUnsupported.
func NewSource() Source {
	return source{}
}

// Start reports ErrUnsupported, so that the Monitor keeps polling.
func (source) Start(func()) error {
	return ErrUnsupported
}

// Close does nothing.
func (source) Close() error {
	return nil
}

// RunLoop does nothing, because there is no notification run loop.
func RunLoop() {}
