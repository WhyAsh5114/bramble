package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/wgnode"
)

// Store is the daemon's directory (default ~/.bramble). Files holding a key
// are mode 0600 and the directory is 0700.
type Store struct {
	Dir string
}

// Identity is this device's WireGuard key. It never leaves the device.
type Identity struct {
	PrivateKeyHex string    `json:"privateKey"`
	CreatedAt     time.Time `json:"createdAt"`
}

// PublicKeyHex derives the public key.
func (i Identity) PublicKeyHex() (string, error) { return wgnode.PublicKeyFromPrivateHex(i.PrivateKeyHex) }

const (
	identityFile = "identity.json"
	stateFile    = "state.json"
	uiURLFile    = "ui.url"
	pidFile      = "bramble.pid"
	logDir       = "logs"
	cacheFile    = "registry-cache.json"
)

// DefaultDir is $BRAMBLE_HOME or ~/.bramble.
func DefaultDir() (string, error) {
	if v := os.Getenv("BRAMBLE_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, ".bramble"), nil
}

// Ensure creates the directory.
func (s *Store) Ensure() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", s.Dir, err)
	}
	return os.Chmod(s.Dir, 0o700)
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// CachePath is where the registry cache is saved.
func (s *Store) CachePath() string { return s.path(cacheFile) }

func (s *Store) writeFile(name string, data []byte) error {
	tmp := s.path(name + ".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

func (s *Store) readJSON(name string, v any) (bool, error) {
	raw, err := os.ReadFile(s.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("%s is not valid JSON: %w", name, err)
	}
	return true, nil
}

func (s *Store) writeJSON(name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(name, append(raw, '\n'))
}

// LoadOrCreateIdentity returns the device key, creating it on first use.
func (s *Store) LoadOrCreateIdentity() (Identity, error) {
	var id Identity
	ok, err := s.readJSON(identityFile, &id)
	if err != nil {
		return id, err
	}
	if ok && id.PrivateKeyHex != "" {
		return id, nil
	}
	priv, _, err := wgnode.GenerateKeyPair()
	if err != nil {
		return id, err
	}
	id = Identity{PrivateKeyHex: priv, CreatedAt: time.Now().UTC()}
	return id, s.writeJSON(identityFile, id)
}

// LoadSaved reads the persisted daemon state.
func (s *Store) LoadSaved() (Saved, error) {
	var st Saved
	_, err := s.readJSON(stateFile, &st)
	return st, err
}

// SaveSaved writes the persisted daemon state.
func (s *Store) SaveSaved(st Saved) error { return s.writeJSON(stateFile, st) }

// ClearCache removes the registry cache, for a new tailnet.
func (s *Store) ClearCache() { _ = os.Remove(s.CachePath()) }

// UIURL returns the running daemon's UI address, if one wrote it.
func (s *Store) UIURL() string {
	raw, err := os.ReadFile(s.path(uiURLFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// SaveRuntime records the UI address and process ID.
func (s *Store) SaveRuntime(uiURL string, pid int) error {
	if err := s.writeFile(uiURLFile, []byte(uiURL+"\n")); err != nil {
		return err
	}
	return s.writeFile(pidFile, []byte(strconv.Itoa(pid)+"\n"))
}

// ClearRuntime removes the runtime files.
func (s *Store) ClearRuntime() {
	_ = os.Remove(s.path(uiURLFile))
	_ = os.Remove(s.path(pidFile))
}

// PID returns the recorded daemon process ID, or 0.
func (s *Store) PID() int {
	raw, err := os.ReadFile(s.path(pidFile))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return pid
}

// OpenLog opens a log file under logs/ for appending.
func (s *Store) OpenLog(name string) (*os.File, error) {
	if err := os.MkdirAll(s.path(logDir), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(s.path(filepath.Join(logDir, name)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// RemoveAll deletes the directory, key included.
func (s *Store) RemoveAll() error { return os.RemoveAll(s.Dir) }
