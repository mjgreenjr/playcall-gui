package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"playcall/internal/espn"
)

// DivisionCache keeps each pro sport's division records (from the standings)
// for the day: the picker and the game page show them next to the overall
// record, and standings change only when a game ends, so one fetch a day is
// enough. Refresh forces another one, for the end of a game. A fetch that
// fails leaves what is on disk, however old.
type DivisionCache struct {
	Dir     string // one file per sport, e.g. ~/.cache/playcall/standings
	Fetcher espn.Fetcher

	mu  sync.Mutex
	mem map[string]divisionFile
}

type divisionFile struct {
	Date    string            `json:"date"` // local day it was fetched, YYYY-MM-DD
	Records map[string]string `json:"records"`
}

const dayLayout = "2006-01-02"

func NewDivisionCache(dir string, f espn.Fetcher) *DivisionCache {
	return &DivisionCache{Dir: dir, Fetcher: f, mem: map[string]divisionFile{}}
}

func (c *DivisionCache) path(sport string) string { return filepath.Join(c.Dir, sport+".json") }

// load reads the sport's file into memory unless it is there already.
func (c *DivisionCache) load(sport string) divisionFile {
	if f, ok := c.mem[sport]; ok {
		return f
	}
	var f divisionFile
	if b, err := os.ReadFile(c.path(sport)); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	c.mem[sport] = f
	return f
}

// Load returns the sport's division records by abbreviation: today's if they
// were fetched today, else a fresh fetch, else whatever is saved. It is nil
// for a sport without divisions, or when nothing was ever fetched. It may use
// the network, so call it off the UI goroutine.
func (c *DivisionCache) Load(ctx context.Context, sport string, now time.Time) map[string]string {
	if !espn.HasDivisionRecords(sport) {
		return nil
	}
	c.mu.Lock()
	f := c.load(sport)
	c.mu.Unlock()
	if f.Date == now.Format(dayLayout) && len(f.Records) > 0 {
		return f.Records
	}
	return c.Refresh(ctx, sport, now)
}

// Refresh fetches the standings again and saves them. On failure it returns
// what was saved before (possibly nil).
func (c *DivisionCache) Refresh(ctx context.Context, sport string, now time.Time) map[string]string {
	if !espn.HasDivisionRecords(sport) {
		return nil
	}
	records, err := espn.FetchDivisionRecords(ctx, c.Fetcher, sport)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		slog.Warn("Could not refresh the standings", "sport", sport, "err", err)
		return c.load(sport).Records
	}
	f := divisionFile{Date: now.Format(dayLayout), Records: records}
	c.mem[sport] = f
	if b, err := json.Marshal(f); err == nil {
		if err := os.MkdirAll(c.Dir, 0o755); err == nil {
			err = os.WriteFile(c.path(sport), b, 0o644)
		}
		if err != nil {
			slog.Warn("Could not save the standings", "sport", sport, "err", err)
		}
	}
	slog.Debug("Division records refreshed", "sport", sport, "teams", len(records))
	return records
}

// Cached is a team's division record from memory only ("" if not loaded yet),
// so it is safe on the UI goroutine.
func (c *DivisionCache) Cached(sport, abbr string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mem[sport].Records[abbr]
}

// AddDivisionRecords fills the games' conference slot with the team's division
// record where ESPN sent no conference record (the pro sports).
func AddDivisionRecords(games []espn.Game, records map[string]string) {
	for i := range games {
		g := &games[i]
		if g.AwayConf == "" {
			g.AwayConf = records[strings.TrimSpace(g.Away)]
		}
		if g.HomeConf == "" {
			g.HomeConf = records[strings.TrimSpace(g.Home)]
		}
	}
}
