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

// DefaultPaths puts the files where the system keeps them: under
// os.UserConfigDir and os.UserCacheDir, which follow XDG_CONFIG_HOME and
// XDG_CACHE_HOME and are ~/.config and ~/.cache when those are not set (the
// same place as always on a normal Linux setup). A user who has set them to
// somewhere else but still has files in the old ~/.config/playcall or
// ~/.cache/playcall keeps using those until the new folder exists, so nothing
// they already have is lost.
func DefaultPaths() (Paths, error) {
	return resolvePaths(os.UserConfigDir, os.UserCacheDir, os.UserHomeDir, dirExists)
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// resolvePaths is DefaultPaths with the system lookups injected for tests.
func resolvePaths(userConfig, userCache, userHome func() (string, error), exists func(string) bool) (Paths, error) {
	cfg, err := userConfig()
	if err != nil {
		return Paths{}, err
	}
	cache, err := userCache()
	if err != nil {
		return Paths{}, err
	}
	p := Paths{ConfigDir: filepath.Join(cfg, "playcall"), CacheDir: filepath.Join(cache, "playcall")}
	if home, err := userHome(); err == nil {
		legacy := PathsFromHome(home)
		p.ConfigDir = keepLegacy(p.ConfigDir, legacy.ConfigDir, exists)
		p.CacheDir = keepLegacy(p.CacheDir, legacy.CacheDir, exists)
	}
	return p, nil
}

// keepLegacy is the old folder when the new one does not exist yet and the old
// one does, else the new one.
func keepLegacy(dir, legacy string, exists func(string) bool) string {
	if dir != legacy && !exists(dir) && exists(legacy) {
		return legacy
	}
	return dir
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
