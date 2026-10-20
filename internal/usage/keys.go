package usage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"kero/internal/proto"
)

var keysMu sync.Mutex

func keysPath() string { return filepath.Join(proto.Dir(), "usage-credentials.json") }

func savedKey(id string) string {
	keysMu.Lock()
	defer keysMu.Unlock()
	var keys map[string]string
	readJSON(keysPath(), &keys)
	return keys[id]
}

// SaveKey stores only explicitly entered API keys in this app's private data
// directory. It does not change any agent's sign-in or configuration.
// An empty key removes the override.
func SaveKey(id, key string) error {
	if id != "deepseek" && id != "opencode" {
		return errors.New("Unsupported API key provider")
	}
	key = strings.TrimSpace(key)
	if len(key) > 8192 || strings.ContainsAny(key, "\x00\r\n") {
		return errors.New("Invalid API key")
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	keys := map[string]string{}
	if err := readJSON(keysPath(), &keys); err != nil && !os.IsNotExist(err) {
		return errors.New("Could not read saved API keys")
	}
	if keys == nil {
		keys = map[string]string{}
	}
	if key == "" {
		delete(keys, id)
	} else {
		keys[id] = key
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(proto.Dir(), 0700); err != nil {
		return errors.New("Could not create private app folder")
	}
	f, err := os.CreateTemp(proto.Dir(), ".usage-keys-*")
	if err != nil {
		return errors.New("Could not save API key")
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("Could not save API key")
	}
	if err := os.Rename(f.Name(), keysPath()); err != nil {
		return errors.New("Could not save API key")
	}
	return nil
}
