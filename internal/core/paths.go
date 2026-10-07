// Package core holds the pieces shared by every front end: paths, config,
// persisted game state and logging. It never prints or exits.
package core

import (
	"os"
	"path/filepath"
)

// Paths locates playcall's files. The base directories are injectable so tests
// never touch the real home directory.
type Paths struct {
	ConfigDir string // e.g. ~/.config/playcall
	CacheDir  string // e.g. ~/.cache/playcall
}

// PathsFromHome returns the standard layout under home, as the Python does
// (~/.config/playcall and ~/.cache/playcall).
func PathsFromHome(home string) Paths {
	return Paths{
		ConfigDir: filepath.Join(home, ".config", "playcall"),
		CacheDir:  filepath.Join(home, ".cache", "playcall"),
	}
}

// DefaultPaths uses the current user's home directory.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return PathsFromHome(home), nil
}

// ConfigFile is config.json.
func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.json") }

// StateFile is state.json.
func (p Paths) StateFile() string { return filepath.Join(p.CacheDir, "state.json") }

// LogFile is playcall.log.
func (p Paths) LogFile() string { return filepath.Join(p.CacheDir, "playcall.log") }

// EnsureDirs creates the config and cache directories.
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.ConfigDir, p.CacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
