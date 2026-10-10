package core

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"playcall/internal/espn"
)

// Status is the three-state result of one poll (Python's None/False/True).
type Status int

const (
	// FetchFailed: no data this time (Python None). The loop backs off.
	FetchFailed Status = iota
	// InProgress: data was read but the game is not finished, or something
	// is still unsent (Python False).
	InProgress
	// Finished: the game is final and everything, the final event included,
	// was sent (Python True).
	Finished
)

// String renders the status like Python's repr of the poll result.
func (s Status) String() string {
	switch s {
	case FetchFailed:
		return "None"
	case Finished:
		return "True"
	}
	return "False"
}

// Outcome is what one poll reports.
type Outcome struct {
	Status Status
	// UntilStart is set for a game that has not started (status "pre") and has
	// a scheduled start; the loop sleeps on it. It can be negative.
	UntilStart *time.Duration
}

// Poller runs polls for one game. Not safe for concurrent use.
type Poller struct {
	Sport string
	Rules Rules
	Sink  Sink
	// StatusSink, when set, gets a GameStatus after every successful fetch.
	StatusSink StatusSink
	// Fetch returns the game's play-by-play. An error means the fetch failed.
	Fetch func(ctx context.Context) (espn.Data, error)
	State *State
	// Store persists State after each send and at the end of a poll. Nil
	// never saves; Once never saves either.
	Store *StateStore
	// Once makes a poll a standalone check: no state is written.
	Once bool
	// Trace, when set (--dry-run), gets the "[DRY-RUN] N new play(s)" header
	// and the indented play lines.
	Trace io.Writer
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// NewPoller returns a poller for state's sport, or an error (ErrNotImplemented
// for sports that are not ported yet) before any polling starts.
func NewPoller(state *State, sink Sink, fetch func(context.Context) (espn.Data, error)) (*Poller, error) {
	rules, err := RulesFor(state.Sport)
	if err != nil {
		return nil, err
	}
	return &Poller{Sport: state.Sport, Rules: rules, Sink: sink, Fetch: fetch, State: state}, nil
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Poller) save() error {
	if p.Once || p.Store == nil {
		return nil
	}
	return p.Store.Save(p.State)
}

// Poll runs one poll: fetch, find the plays not announced yet, send them
// (scoring updates first), advance the state, and send the final event once
// the game is over and everything else is out. The error is for unexpected
// failures (a bad play id, a state file that cannot be written); a failed
// fetch or send is not one, it is reported through the Outcome.
func (p *Poller) Poll(ctx context.Context) (Outcome, error) {
	st := p.State
	data, err := p.Fetch(ctx)
	if err != nil || data == nil {
		slog.Warn("Failed to fetch data", "game", st.GameID, "err", err)
		return Outcome{Status: FetchFailed}, nil
	}
	plays := espn.ExtractPlays(data)
	finished := espn.IsFinal(data)
	feed := p.newFeed(data, plays, finished)
	p.publishStatus(feed)
	var out Outcome
	if len(plays) == 0 {
		out.UntilStart = secondsUntilStart(data, p.now())
		slog.Debug("No plays", "game", st.GameID)
		if finished && p.sendFinal(ctx, feed) {
			out.Status = Finished
		} else {
			out.Status = InProgress
		}
		return out, nil
	}
	out.Status = InProgress

	track := p.Rules.TrackSent()
	var unseen []espn.Data
	var sent map[string]bool
	if track {
		// Sent-id sports: new means "id not in sent_ids", in list order.
		sent = make(map[string]bool, len(st.SentIDs))
		for _, id := range st.SentIDs {
			sent[id] = true
		}
		feed.SentIDs = sent
		unseen = unsentPlays(plays, sent)
	} else {
		var err error
		if unseen, err = unseenPlays(plays, st.LastPlayID); err != nil {
			return out, err
		}
	}
	rules := p.Rules.Prepare(feed)
	unseen = filterPlays(unseen, rules.Announced)

	// First poll announces only the latest play; after a gap, the newest few.
	limit := 1
	if st.LastPlayID != nil {
		limit = p.Rules.CatchupLimit()
	}
	skipped, toAnnounce := splitCatchup(unseen, limit)
	if len(skipped) > 0 {
		slog.Debug("Skipping older plays", "count", len(skipped), "game", st.GameID)
		id, _ := espn.PlayID(skipped[len(skipped)-1])
		last := PlayID(id)
		st.LastPlayID = &last
		if track {
			markSent(st, sent, rules, skipped)
		}
	}

	if st.ScoringTexts == nil {
		st.ScoringTexts = map[string]string{}
	}
	seen := st.ScoringTexts
	pending := map[string]bool{}
	for _, pl := range toAnnounce {
		pending[espn.PlayKey(pl)] = true
	}
	updates := filterPlays(findScoringUpdates(seen, plays, pending), rules.Announced)
	toSend := map[string]bool{}
	for k := range pending {
		toSend[k] = true
	}
	for _, pl := range updates {
		toSend[espn.PlayKey(pl)] = true
	}
	// Scoring plays not being sent this poll get their current text as baseline.
	for _, pl := range plays {
		if espn.Truthy(pl["scoringPlay"]) && !toSend[espn.PlayKey(pl)] {
			seen[espn.PlayKey(pl)] = espn.PlayText(pl)
		}
	}

	if p.Trace != nil {
		fmt.Fprintf(p.Trace, "[DRY-RUN] %d new play(s) for game %s\n", len(toAnnounce), st.GameID)
		for _, pl := range toAnnounce {
			fmt.Fprintf(p.Trace, "  %s: %s\n", espn.PlayKey(pl), espn.PlayText(pl))
		}
	}

	allSent := true
	// Re-send scoring plays that changed (they happened earlier), then new plays.
	type send struct {
		play   espn.Data
		update bool
	}
	var sends []send
	for _, pl := range updates {
		sends = append(sends, send{pl, true})
	}
	for _, pl := range toAnnounce {
		sends = append(sends, send{pl, false})
	}
	for _, s := range sends {
		text := espn.PlayText(s.play)
		kind := KindPlay
		if s.update {
			kind = KindUpdate
		}
		slog.Debug("Play", "kind", kind, "id", espn.PlayKey(s.play), "text", text)
		msg := rules.Message(s.play)
		ev := Event{
			GameID:   st.GameID,
			PlayID:   espn.PlayKey(s.play),
			Kind:     kind,
			Title:    withSeries(msg.Title, feed.Series),
			Body:     msg.Body,
			Urgent:   msg.Urgent,
			URL:      espn.GamePageURL(p.Sport, st.GameID),
			Away:     msg.Away,
			Home:     msg.Home,
			Clock:    msg.Clock,
			Period:   msg.Period,
			PlayText: text,
			Sport:    p.Sport,
			Series:   feed.Series,
		}
		if err := p.Sink.Send(ctx, ev); err != nil {
			// Stop here; the play stays unsent and the next poll retries it.
			slog.Error("Failed to send event", "game", st.GameID, "play", ev.PlayID, "err", err)
			allSent = false
			break
		}
		if espn.Truthy(s.play["scoringPlay"]) {
			seen[espn.PlayKey(s.play)] = text
		}
		if !s.update {
			id, _ := espn.PlayID(s.play)
			last := PlayID(id)
			st.LastPlayID = &last
			if track {
				markSent(st, sent, rules, []espn.Data{s.play})
			}
		}
		if err := p.save(); err != nil {
			return out, err
		}
	}
	// --once is a standalone check: save is a no-op for it.
	if err := p.save(); err != nil {
		return out, err
	}
	if finished && allSent && p.sendFinal(ctx, feed) {
		out.Status = Finished
	}
	return out, nil
}

// newFeed collects the game-level values: team abbreviations and scores, and
// the clock and period (top-level if present, else the status').
func (p *Poller) newFeed(data espn.Data, plays []espn.Data, finished bool) *Feed {
	f := &Feed{Data: data, Plays: plays, Finished: finished}
	home, away := espn.Teams(espn.Competition(data))
	if home == nil || away == nil {
		f.Away = TeamScore{Abbr: "UNK", Score: "0"}
		f.Home = TeamScore{Abbr: "UNK", Score: "0"}
	} else {
		f.Away = TeamScore{Abbr: espn.TeamAbbr(away), Name: espn.Str(espn.Map(away["team"])["displayName"]), Score: espn.ScoreText(away["score"]), Logo: espn.TeamLogos(espn.Map(away["team"]))}
		f.Home = TeamScore{Abbr: espn.TeamAbbr(home), Name: espn.Str(espn.Map(home["team"])["displayName"]), Score: espn.ScoreText(home["score"]), Logo: espn.TeamLogos(espn.Map(home["team"]))}
	}
	status := espn.Status(data)
	f.Clock = firstText(data["clock"], status["displayClock"])
	f.Period = firstText(data["period"], status["displayPeriod"])
	f.Series = seriesText(data, "", false)
	return f
}

// firstText is Python's `a or b` for text values.
func firstText(a, b any) string {
	if espn.Truthy(a) {
		return espn.ScoreText(a)
	}
	return espn.Str(b)
}

// sendFinal sends the end-of-game event ("ATL 24 · Final · NO 21"). It
// returns true if sent, or if there is nothing to send (no two teams).
func (p *Poller) sendFinal(ctx context.Context, f *Feed) bool {
	home, away := espn.Teams(espn.Competition(f.Data))
	if home == nil || away == nil {
		return true
	}
	detail := espn.Str(espn.Map(espn.Status(f.Data)["type"])["shortDetail"])
	if detail == "" {
		detail = "Final"
	}
	st := p.State
	series := seriesText(f.Data, st.GameID, true)
	title := withSeries(FormatTitle(f.Away.Abbr, f.Away.Score, detail, "", f.Home.Abbr, f.Home.Score), series)
	slog.Debug("Final event", "game", st.GameID, "title", title)
	err := p.Sink.Send(ctx, Event{
		GameID: st.GameID,
		Kind:   KindFinal,
		Title:  title,
		Body:   "Game over",
		Urgent: true,
		URL:    espn.GamePageURL(p.Sport, st.GameID),
		Away:   f.Away,
		Home:   f.Home,
		Period: detail,
		Sport:  p.Sport,
		Series: series,
	})
	if err != nil {
		slog.Error("Failed to send final event", "game", st.GameID, "err", err)
		return false
	}
	return true
}

// unseenPlays returns the plays after last, oldest first, ids compared as
// integers (they are not all the same width). Every play needs an integer id.
func unseenPlays(plays []espn.Data, last *PlayID) ([]espn.Data, error) {
	type item struct {
		id   int64
		play espn.Data
	}
	var items []item
	for _, pl := range plays {
		id, ok := espn.PlayID(pl)
		if !ok {
			return nil, fmt.Errorf("play id %v is not an integer", pl["id"])
		}
		if last == nil || id > int64(*last) {
			items = append(items, item{id, pl})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].id < items[j].id })
	out := make([]espn.Data, len(items))
	for i, it := range items {
		out[i] = it.play
	}
	return out, nil
}

// unsentPlays returns the plays whose id is not in sent, in list order (never
// compared to last_play_id: NHL and basketball ids are not chronological).
func unsentPlays(plays []espn.Data, sent map[string]bool) []espn.Data {
	return filterPlays(plays, func(p espn.Data) bool { return !sent[espn.PlayKey(p)] })
}

// markSent adds the ids of plays (each play's own, plus any bundled with it) to
// sent and writes them to the state as a sorted list of strings.
func markSent(st *State, sent map[string]bool, rules PlayRules, plays []espn.Data) {
	for _, pl := range plays {
		for _, id := range rules.IDs(pl) {
			sent[id] = true
		}
	}
	ids := make([]string, 0, len(sent))
	for id := range sent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	st.SentIDs = ids
}

// splitCatchup keeps the newest limit plays to announce and returns the older
// ones as skipped. Shared by the id-compare and sent-id paths.
func splitCatchup(unseen []espn.Data, limit int) (skipped, announce []espn.Data) {
	n := len(unseen) - limit
	if n < 0 {
		n = 0
	}
	return unseen[:n], unseen[n:]
}

func filterPlays(plays []espn.Data, keep func(espn.Data) bool) []espn.Data {
	var out []espn.Data
	for _, p := range plays {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}

// findScoringUpdates returns scoring plays whose text differs from the text
// last sent for them (e.g. a PAT result added to the touchdown play under the
// same id). Plays in pending are being sent as new plays this poll, so they
// are not updates.
func findScoringUpdates(seen map[string]string, plays []espn.Data, pending map[string]bool) []espn.Data {
	var out []espn.Data
	for _, p := range plays {
		k := espn.PlayKey(p)
		old, ok := seen[k]
		if espn.Truthy(p["scoringPlay"]) && ok && old != espn.PlayText(p) && !pending[k] {
			out = append(out, p)
		}
	}
	return out
}

// startLayout is ESPN's timestamp format, with no seconds.
const startLayout = "2006-01-02T15:04Z"

// secondsUntilStart returns the time until a scheduled ("pre") game starts,
// or nil if it is not scheduled or has no usable start time.
func secondsUntilStart(data espn.Data, now time.Time) *time.Duration {
	state := espn.Str(espn.Map(espn.Status(data)["type"])["state"])
	if state != "pre" {
		return nil
	}
	start, err := time.Parse(startLayout, espn.Str(espn.Competition(data)["date"]))
	if err != nil {
		return nil
	}
	d := start.Sub(now)
	return &d
}
