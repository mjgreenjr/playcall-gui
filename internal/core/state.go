package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// PlayID is a play id that is compared as an integer. In state.json it is
// written as a string, as the Python does with ESPN's string ids, and read
// from either a string or a number.
type PlayID int64

// MarshalJSON writes the id as a JSON string.
func (id PlayID) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(id), 10))
}

// UnmarshalJSON accepts "123" or 123.
func (id *PlayID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid play id %s", b)
	}
	*id = PlayID(n)
	return nil
}

// State is state.json, the followed game and how far it has been announced.
// The format is shared with the Python version.
type State struct {
	GameID string `json:"game_id"`
	Sport  string `json:"sport"`
	// LastPlayID is nil until a play has been sent or skipped.
	LastPlayID *PlayID `json:"last_play_id"`
	// StartedAt is a Unix time in seconds (Python's time.time()).
	StartedAt float64 `json:"started_at"`
	// ScoringTexts maps play id to the last seen text of scoring plays.
	// nil is left out of the file; empty is written as {}.
	ScoringTexts map[string]string `json:"scoring_texts"`
	// SentIDs lists every announced or skipped play id (NHL, NBA, WNBA, cbb).
	// nil is left out of the file; empty is written as [].
	SentIDs []string `json:"sent_ids"`
}

// stateFile fixes the key order (alphabetical, like sort_keys) and lets nil
// ScoringTexts/SentIDs be omitted while empty ones are kept. A null would break
// the Python reader.
type stateFile struct {
	GameID       string             `json:"game_id"`
	LastPlayID   *PlayID            `json:"last_play_id"`
	ScoringTexts *map[string]string `json:"scoring_texts,omitempty"`
	SentIDs      *[]string          `json:"sent_ids,omitempty"`
	Sport        string             `json:"sport"`
	StartedAt    float64            `json:"started_at"`
}

// MarshalJSON implements json.Marshaler.
func (s State) MarshalJSON() ([]byte, error) {
	f := stateFile{GameID: s.GameID, LastPlayID: s.LastPlayID, Sport: s.Sport, StartedAt: s.StartedAt}
	if s.ScoringTexts != nil {
		f.ScoringTexts = &s.ScoringTexts
	}
	if s.SentIDs != nil {
		f.SentIDs = &s.SentIDs
	}
	return json.Marshal(f)
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *State) UnmarshalJSON(b []byte) error {
	var f stateFile
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*s = State{GameID: f.GameID, LastPlayID: f.LastPlayID, Sport: f.Sport, StartedAt: f.StartedAt}
	if f.ScoringTexts != nil {
		s.ScoringTexts = *f.ScoringTexts
	}
	if f.SentIDs != nil {
		s.SentIDs = *f.SentIDs
	}
	return nil
}

// StateStore reads and writes state.json. It remembers the text of the last
// write so an unchanged state is not written again.
type StateStore struct {
	path  string
	saved string
	have  bool
}

// NewStateStore returns a store for the state file under p.
func NewStateStore(p Paths) *StateStore {
	return &StateStore{path: p.StateFile()}
}

// Load returns the saved state, or nil (and no error) when there is none.
func (s *StateStore) Load() (*State, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	return &st, nil
}

// Save writes the state unless it is identical to the last one written.
func (s *StateStore) Save(st *State) error {
	b, err := marshalIndent(st)
	if err != nil {
		return err
	}
	text := string(b)
	if s.have && text == s.saved {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, b, 0o644); err != nil {
		return err
	}
	s.saved, s.have = text, true
	last := "none"
	if st.LastPlayID != nil {
		last = strconv.FormatInt(int64(*st.LastPlayID), 10)
	}
	slog.Info(fmt.Sprintf("State saved: game %s, last play %s", st.GameID, last))
	return nil
}

// Clear removes the state file, if there is one.
func (s *StateStore) Clear() error {
	err := os.Remove(s.path)
	s.saved, s.have = "", false
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	slog.Info("State cleared")
	return nil
}
