package linear

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"kero/internal/proto"
)

var credsMu sync.Mutex

// credsFile is where the workspace's key is kept, in the app's own
// private data directory, beside the sessions and the state.
func credsFile() string {
	return filepath.Join(proto.Dir(), "credentials.json")
}

// Key is the workspace's API key: from the environment when it is set,
// which is how a headless run has one, or from the app's own private
// data directory.
//
// The file is read on every call rather than kept, so that replacing it
// — which is what rotating a key is — takes effect at once, and so that
// the key is not held in memory longer than a request needs it.
func Key() string {
	credsMu.Lock()
	defer credsMu.Unlock()
	if k := strings.TrimSpace(os.Getenv("LINEAR_API_KEY")); k != "" {
		return k
	}
	b, err := os.ReadFile(credsFile())
	if err != nil {
		return ""
	}
	var creds map[string]string
	if json.Unmarshal(b, &creds) != nil {
		return ""
	}
	return strings.TrimSpace(creds["linear"])
}

// SaveKey keeps the workspace's key, and nothing else, in the app's
// private data directory. An empty key removes it.
func SaveKey(key string) error {
	credsMu.Lock()
	defer credsMu.Unlock()
	key = strings.TrimSpace(key)
	if len(key) > 8192 || strings.ContainsAny(key, "\x00\r\n") {
		return os.ErrInvalid
	}
	dir := proto.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	creds := map[string]string{}
	if b, err := os.ReadFile(credsFile()); err == nil {
		if json.Unmarshal(b, &creds) != nil {
			return errors.New("could not read saved credentials; existing file was preserved")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("could not read saved credentials; existing file was preserved")
	}
	if creds == nil {
		creds = map[string]string{}
	}
	if key == "" {
		delete(creds, "linear")
	} else {
		creds["linear"] = key
	}
	b, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	// The key is written in place, through a temporary file, so that a
	// crash cannot leave a half-written one behind.
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, credsFile())
}
