package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// HalfStore keeps each college game's first-half timeout totals in a small
// file next to state.json, so a restart in the second half still knows how
// many each side had used at the break (see Poller.collegeFirstHalf). The GUI
// does not use state.json, and the file is not part of its format: a separate
// cache file, keyed by game id, that the CLI and the Python never read.
type HalfStore struct {
	path string
	now  func() time.Time
}

// FirstHalf is what a game's entry holds: ESPN's `timeoutsUsed` per side, and
// whether it was taken at the break (Final) or still in play, when the
// total can still rise.
type FirstHalf struct {
	Away  int64 `json:"away"`
	Home  int64 `json:"home"`
	Final bool  `json:"final"`
	// At is a Unix time in seconds, to drop old games.
	At int64 `json:"at"`
}

// halfKeep is how long a game's entry stays in the file.
const halfKeep = 7 * 24 * time.Hour

// NewHalfStore returns a store for the file under p.
func NewHalfStore(p Paths) *HalfStore {
	return &HalfStore{path: filepath.Join(p.CacheDir, "first_half.json")}
}

func (s *HalfStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *HalfStore) read() map[string]FirstHalf {
	games := map[string]FirstHalf{}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return games
	}
	var f struct {
		Games map[string]FirstHalf `json:"games"`
	}
	if json.Unmarshal(b, &f) == nil && f.Games != nil {
		games = f.Games
	}
	return games
}

// Load returns the saved totals for a game.
func (s *HalfStore) Load(gameID string) (FirstHalf, bool) {
	h, ok := s.read()[gameID]
	return h, ok
}

// Save writes a game's totals and drops entries older than a week.
func (s *HalfStore) Save(gameID string, h FirstHalf) error {
	now := s.clock()
	h.At = now.Unix()
	games := s.read()
	games[gameID] = h
	for id, g := range games {
		if now.Sub(time.Unix(g.At, 0)) > halfKeep {
			delete(games, id)
		}
	}
	b, err := marshalIndent(map[string]any{"games": games})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	err = os.WriteFile(s.path, b, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
