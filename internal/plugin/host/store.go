package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// A plugin's folder holds what the user chose for it, and what it was
// trusted with:
//
//	settings.json  the values of its manifest's fields
//	secrets.json   the values of the fields it marked secret
//
// The two are apart because only the first ever leaves the app for the
// plugin; a secret reaches the plugin only when it asks for it by name, if
// its manifest grants the secrets permission.
const (
	settingsFile = "settings.json"
	secretsFile  = "secrets.json"
)

// Store keeps the settings and secrets of plugins, under the app's plugin
// folder. Each call reads the files anew, as credentials do: an edit by
// hand shows up on the next read, and the lock covers a read, a change and
// a write as one.
type Store struct {
	mu  sync.Mutex
	dir string
}

// NewStore keeps plugins' files under dir: proto.Dir()/plugins.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// validName tells a name that is safe as one folder: a plugin's id, or a
// setting's key.
func validName(s string) bool {
	if s == "" || len(s) > 128 || s[0] == '.' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// folder is where a plugin's files live, made when missing.
func (s *Store) folder(plugin string) (string, error) {
	if !validName(plugin) {
		return "", fmt.Errorf("invalid plugin id %q", plugin)
	}
	dir := filepath.Join(s.dir, plugin)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// read reads a plugin's file as a map. A missing file is an empty map; a
// file that does not parse is an error, and is left as it is.
func (s *Store) read(plugin, name string) (map[string]string, error) {
	dir, err := s.folder(plugin)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal(b, &values); err != nil {
		return nil, fmt.Errorf("could not read %s for plugin %q; the existing file was preserved", name, plugin)
	}
	return values, nil
}

// write writes a plugin's file as one atomic replacement, readable by its
// owner alone, in a folder no one else can enter.
func (s *Store) write(plugin, name string, values map[string]string) error {
	dir, err := s.folder(plugin)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(dir, "."+strings.TrimSuffix(name, ".json")+"-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(dir, name))
}

// Settings returns a plugin's saved settings. Values the plugin's manifest
// does not declare are not filtered here: the settings page writes only
// what the manifest declares, and the plugin reads only what it asks for.
func (s *Store) Settings(plugin string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read(plugin, settingsFile)
	if err != nil {
		return map[string]string{}
	}
	return values
}

// SetSettings saves a plugin's settings, keeping the values it does not
// mention.
func (s *Store) SetSettings(plugin string, values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.read(plugin, settingsFile)
	if err != nil {
		return err
	}
	for k, v := range values {
		if !validName(k) {
			return fmt.Errorf("invalid setting %q", k)
		}
		old[k] = v
	}
	return s.write(plugin, settingsFile, old)
}

// Secret returns one of a plugin's secrets, empty when it has none.
func (s *Store) Secret(plugin, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read(plugin, secretsFile)
	if err != nil {
		return ""
	}
	return values[key]
}

// HasSecret tells whether a plugin has a secret set, without returning it.
func (s *Store) HasSecret(plugin, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read(plugin, secretsFile)
	if err != nil {
		return false
	}
	_, ok := values[key]
	return ok
}

// SetSecret saves one of a plugin's secrets. An empty value forgets it.
func (s *Store) SetSecret(plugin, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validName(key) {
		return fmt.Errorf("invalid secret %q", key)
	}
	if len(value) > 64<<10 || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("the value of %q is too long or has a line break", key)
	}
	old, err := s.read(plugin, secretsFile)
	if err != nil {
		return err
	}
	if value == "" {
		delete(old, key)
	} else {
		old[key] = value
	}
	return s.write(plugin, secretsFile, old)
}
