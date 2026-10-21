package host

import (
	"os"
	"path/filepath"
	"sync"
)

// logLimit caps a plugin's log. At the cap the log rolls over once, to
// <id>.log.1, and the plugin keeps writing into an empty <id>.log. The
// stderr of a plugin that talks much loses its oldest lines; the newest
// lines are the ones a crash needs.
const logLimit = 1 << 20

// logWriter writes a plugin's stderr to its log, off the UI goroutine. It
// is safe for concurrent use.
type logWriter struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

// openLog opens the log of a plugin, under dir, making the folders it
// needs.
func openLog(dir, id string) (*logWriter, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &logWriter{path: filepath.Join(dir, id+".log")}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// open opens the log for appending, or starts a new one when the old one
// filled.
func (w *logWriter) open() error {
	if info, err := os.Stat(w.path); err == nil && info.Size() >= logLimit {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

// rotate moves the full log aside, over the previous rollover.
//
// The older rollover is the one to lose: a plugin's log is read for the
// crash that just happened.
func (w *logWriter) rotate() error {
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
	return os.Rename(w.path, w.path+".1")
}

// Write appends a plugin's stderr, rolling the log at the cap.
func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > logLimit {
		if err := w.rotate(); err != nil {
			return 0, err
		}
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the log.
func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// Path is where the log is.
func (w *logWriter) Path() string { return w.path }
