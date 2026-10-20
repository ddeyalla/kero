package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// listenSocket never unlinks a live daemon's address. The directory lock in
// Run is insufficient when two data directories inherit the same KERO_SOCKET.
func listenSocket(path string) (*socketListener, error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			lock.Close()
		}
	}()
	if err := lockFile(lock); err != nil {
		return nil, fmt.Errorf("kerod socket is already in use: %w", err)
	}
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("kerod socket path is occupied by a non-socket")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	addr := &net.UnixAddr{Name: path, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		// Older daemons do not hold the socket lock. Only a refused connection
		// to an unchanged socket proves that its leftover address is stale.
		st, statErr := os.Lstat(path)
		if statErr != nil || st.Mode()&os.ModeSocket == 0 {
			return nil, err
		}
		conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("kerod socket belongs to a running daemon; refusing to replace it")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("cannot prove kerod socket is stale: %w", dialErr)
		}
		current, statErr := os.Lstat(path)
		if statErr != nil || !os.SameFile(st, current) {
			return nil, errors.New("kerod socket changed during startup; refusing to replace it")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		ln, err = net.ListenUnix("unix", addr)
		if err != nil {
			return nil, err
		}
	}
	// Go's default cleanup unlinks by pathname, even if another process has
	// replaced it. Remember our inode and clean up only our own socket.
	ln.SetUnlinkOnClose(false)
	st, err := os.Lstat(path)
	if err != nil {
		ln.Close()
		return nil, err
	}
	l := &socketListener{UnixListener: ln, path: path, file: st, lock: lock}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	ok = true
	return l, nil
}

type socketListener struct {
	*net.UnixListener
	path string
	file os.FileInfo
	lock *os.File
	once sync.Once
	err  error
}

func (l *socketListener) Close() error {
	l.once.Do(func() {
		l.err = l.UnixListener.Close()
		if st, err := os.Lstat(l.path); err == nil && os.SameFile(l.file, st) {
			os.Remove(l.path)
		}
		// Keep the lock file: unlinking it would allow concurrent holders of
		// different inodes to believe they own the same socket address.
		l.lock.Close()
	})
	return l.err
}
