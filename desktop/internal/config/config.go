// Package config stores the non-secret app settings in the user's config
// directory. The session token is never stored here (see package secrets).
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"tracker/desktop/internal/brand"
)

// Config is the persisted, non-secret configuration.
type Config struct {
	Server string `json:"server"`
	Email  string `json:"email"`
	// InsecureTLS skips certificate verification for Server (the user's
	// explicit choice on the Connect screen, for self-signed servers).
	InsecureTLS bool     `json:"insecure_tls,omitempty"`
	TagFilter   []string `json:"tag_filter"`
}

// Store reads and writes the config file.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store for the default location
// (<UserConfigDir>/<brand.Name>/config.json).
func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, brand.Name, "config.json")}, nil
}

// NewStoreAt returns a Store for an explicit path (tests).
func NewStoreAt(path string) *Store { return &Store{path: path} }

// Load returns the saved config, or an empty one if none exists.
func (s *Store) Load() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Config
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, nil // a corrupt file is treated as empty
	}
	return c, nil
}

// Update applies fn to the saved config and writes it atomically (0600).
func (s *Store) Update(fn func(*Config)) error {
	c, err := s.Load()
	if err != nil {
		return err
	}
	fn(&c)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
