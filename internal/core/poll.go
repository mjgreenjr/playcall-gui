package core

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"playcall/internal/espn"
	"playcall/internal/sports"
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

	// texts is each play's text as of the last poll, to log edits.
	texts map[string]string
	// What logTimeouts has logged: the last counters, the timeout plays seen,
	// and whether the first poll is done.
	lastTimeouts   string
	timeoutPlays   map[string]bool
	timeoutsSeeded bool
	// College timeout labels from the counter (see collegeTimeoutVerdicts).
	timeoutVerdicts map[string]bool
	timeoutSeen     map[string]bool
	verdictsSeeded  bool
	usedSeeded      bool
	lastUsed        [2]int64
	credits         []time.Time
	// known is the football plays this run has seen or sent whose ids are not
	// above last_play_id. ESPN's football ids are not always in play order (a
	// timeout can carry a higher id than the snap that followed it), so a play
	// with an id below last_play_id that is not known is a late one to send.
	// In memory only; the first poll of a run takes everything at or below
	// last_play_id as known.
	known map[string]bool
	// signatures is what each football play looked like (period, clock, type,
	// start of the text), to spot a play ESPN replaced with a new id.
	signatures map[string]string
	// collegeFirstHalf is each college side's timeouts used when the first half
	// ended ("away", "home"), seen while it was in play; see collegeCounts.
	collegeFirstHalf map[string]int64
	// collegeWatched is whether an earlier poll of this run found the game in
	// play; collegeOTPeriod, collegeOTBase and collegeOTKnown are the current
	// overtime period and ESPN's counters when it began (see collegeOvertime).
	collegeWatched  bool
	collegeOTPeriod int64
	collegeOTBase   [2]int64
	collegeOTKnown  bool
	// FirstHalfStore, when set (the GUI), saves those totals and takes them
	// back after a restart in the second half. Once never saves.
	FirstHalfStore *HalfStore
	halfSaved      FirstHalf
	// ended is the periods whose end an earlier poll's feed already showed.
	ended map[int64]bool
	// sentTexts is the text of each revisable play (see revisablePlay) as it
	// was last sent, to re-send the play when ESPN fills in its text later.
	// In memory only: a restart forgets it.
	sentTexts map[string]string
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

// logTextChanges logs, at DEBUG, each play whose text differs from the last
// poll. ESPN sometimes posts a play with partial text and fills it in later
// under the same id; only scoring plays are re-sent when that happens, so this
// is how to tell which plays a sink showed incomplete.
func (p *Poller) logTextChanges(plays []espn.Data) {
	if p.texts == nil {
		p.texts = map[string]string{}
	}
	for _, pl := range plays {
		id, text := espn.PlayKey(pl), espn.PlayText(pl)
		if old, ok := p.texts[id]; ok && old != text {
			slog.Debug("Play text changed", "game", p.State.GameID, "play", id, "old", old, "new", text,
				"scoring", espn.Truthy(pl["scoringPlay"]))
		}
		p.texts[id] = text
	}
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
	startedBeforeStatus(p.Sport, data, plays)
	behind := catchUpScore(p.Sport, data, plays)
	finished := espn.IsFinal(data)
	p.logTextChanges(plays)
	feed := p.newFeed(data, plays, finished)
	feed.ScoreBehind = behind
	p.logTimeouts(feed)
	p.collegeTimeoutVerdicts(feed)
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
		switch p.Rules.(type) {
		case footballRules:
			unseen = p.addLatePlays(plays, unseen, st.LastPlayID)
			unseen = p.dropReplaced(plays, unseen)
			unseen = p.dropAfterPeriodEnd(plays, unseen)
		case mlbRules:
			// ESPN posted "Vesia relieved Yamamoto" after the batter's "pitches
			// to" play, under a lower id.
			unseen = p.addLatePlays(plays, unseen, st.LastPlayID)
		}
	}
	rules := p.Rules.Prepare(feed)
	var lastBefore int64 = -1 // plays at or below it are late (football and baseball only)
	if st.LastPlayID != nil && !track {
		lastBefore = int64(*st.LastPlayID)
	}
	for _, pl := range unseen {
		if !rules.Announced(pl) {
			p.markKnown(pl) // a late play that is never announced is not late again next poll
		}
	}
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
		p.advanceLast(id)
		for _, pl := range skipped {
			p.markKnown(pl)
		}
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
	updates = append(updates, filterPlays(p.findRevisions(plays, pending), rules.Announced)...)
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
		if msg.Display == "" && nameSports[p.Sport] {
			if shown := annotateNames(msg.Body, rosterTeams(feed.Data)); shown != msg.Body {
				msg.Display = shown
			}
		}
		id, idOK := espn.PlayID(s.play)
		at, _ := time.Parse(time.RFC3339, espn.Str(s.play["wallclock"]))
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

			Possession: possession(p.Sport, feed),
			Batter:     msg.Batter,
			Display:    msg.Display,
			Revision:   s.update && (!espn.Truthy(s.play["scoringPlay"]) || stalePlay(plays, s.play)),
			Late:       !s.update && idOK && id <= lastBefore,
			At:         at,
			Ending:     endsGame(s.play),
		}
		if err := p.Sink.Send(ctx, ev); err != nil {
			// Stop here; the play stays unsent and the next poll retries it.
			slog.Error("Failed to send event", "game", st.GameID, "play", ev.PlayID, "err", err)
			allSent = false
			break
		}
		if espn.Truthy(s.play["scoringPlay"]) {
			seen[espn.PlayKey(s.play)] = text
		} else if revisablePlay(p.Sport, s.play) {
			if p.sentTexts == nil {
				p.sentTexts = map[string]string{}
			}
			p.sentTexts[espn.PlayKey(s.play)] = text
		}
		if !s.update {
			id, _ := espn.PlayID(s.play)
			p.advanceLast(id)
			p.markKnown(s.play)
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

// Recheck fetches the game once more and publishes its status, for a game that
// is already final: ESPN can change the teams' records and scores minutes after the final
// status (KENN @ MOST, BUF @ LV), when polling has stopped. It sends no play
// or event, and writes no state. A failed fetch changes nothing.
func (p *Poller) Recheck(ctx context.Context) {
	data, err := p.Fetch(ctx)
	if err != nil || data == nil {
		slog.Warn("Failed to recheck the final", "game", p.State.GameID, "err", err)
		return
	}
	plays := espn.ExtractPlays(data)
	behind := catchUpScore(p.Sport, data, plays)
	feed := p.newFeed(data, plays, espn.IsFinal(data))
	feed.ScoreBehind = behind
	p.publishStatus(feed)
}

// rank is a team's ranking, for the sports that rank their teams.
func (p *Poller) rank(competitor espn.Data) string {
	if !sports.HasRankings(p.Sport) {
		return ""
	}
	return espn.Rank(competitor)
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
		f.Away = TeamScore{Abbr: espn.TeamAbbr(away), Name: espn.Str(espn.Map(away["team"])["displayName"]), Score: espn.ScoreText(away["score"]), Logo: espn.TeamLogos(espn.Map(away["team"])), Rank: p.rank(away)}
		f.Home = TeamScore{Abbr: espn.TeamAbbr(home), Name: espn.Str(espn.Map(home["team"])["displayName"]), Score: espn.ScoreText(home["score"]), Logo: espn.TeamLogos(espn.Map(home["team"])), Rank: p.rank(home)}
	}
	status := espn.Status(data)
	f.Clock = firstText(data["clock"], status["displayClock"])
	f.Period = firstText(data["period"], status["displayPeriod"])
	f.Clock = footballClock(p.Sport, f.Clock, data, plays, finished)
	f.Clock, f.Period = footballOvertime(p.Sport, f.Clock, f.Period, data)
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
		Ending: true,
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

// advanceLast records id as the last play handled. For football and baseball it only moves
// up: a late play with a lower id leaves it where it is. Other sports keep the
// last id sent, whatever it is (their ids are not chronological and last_play_id
// is only for the log).
func (p *Poller) advanceLast(id int64) {
	st := p.State
	onlyUp := false
	switch p.Rules.(type) {
	case footballRules, mlbRules:
		onlyUp = true
	}
	if onlyUp && st.LastPlayID != nil && id <= int64(*st.LastPlayID) {
		return
	}
	last := PlayID(id)
	st.LastPlayID = &last
}

// playSignature identifies a football play by when and what it was, not by id.
func playSignature(pl espn.Data) string {
	n, _ := espn.Int64(espn.Map(pl["period"])["number"])
	text := []rune(espn.PlayText(pl))
	if len(text) > 30 {
		text = text[:30]
	}
	return strconv.FormatInt(n, 10) + "|" + espn.Str(espn.Map(pl["clock"])["displayValue"]) + "|" + playType(pl) + "|" + string(text)
}

// nearReplaced finds an earlier play, gone from the feed, that has the same
// period and the same start of text as the new one (signature sig) but not the
// same clock or type: a replaced play dropReplaced did not catch. It returns
// the old play's id and signature, or "" if there is none.
func nearReplaced(signatures map[string]string, key, sig string, present map[string]bool) (string, string) {
	near := func(s string) string {
		parts := strings.SplitN(s, "|", 4)
		if len(parts) < 4 {
			return s
		}
		return parts[0] + "|" + parts[3]
	}
	for old, s := range signatures {
		if old != key && !present[old] && near(s) == near(sig) {
			return old, s
		}
	}
	return "", ""
}

// dropReplaced takes out of unseen the football plays that are an earlier play
// again under a new id: ESPN corrects a play (a kickoff's "fair catch" became
// "Touchback") by dropping the old id and posting the corrected play with a
// fresh one, which would otherwise be announced a second time, long after the
// original, out of order. They are marked as handled and not sent.
func (p *Poller) dropReplaced(plays, unseen []espn.Data) []espn.Data {
	if p.signatures == nil {
		p.signatures = map[string]string{}
	}
	present := map[string]bool{}
	for _, pl := range plays {
		present[espn.PlayKey(pl)] = true
	}
	var kept []espn.Data
	for _, pl := range unseen {
		key, sig := espn.PlayKey(pl), playSignature(pl)
		replaced := ""
		for old, s := range p.signatures {
			if s == sig && old != key && !present[old] {
				replaced = old
				break
			}
		}
		if replaced == "" {
			// Not the same play by our test; say so when it is close (same
			// period and start of text, a different clock or type) and the
			// old one is gone, to learn what ESPN changed when it swaps an id.
			if old, was := nearReplaced(p.signatures, key, sig, present); old != "" {
				slog.Debug("Play like a replaced one", "game", p.State.GameID, "old", old, "new", key, "was", was, "now", sig)
			}
			kept = append(kept, pl)
			continue
		}
		slog.Debug("Play replaced", "game", p.State.GameID, "old", replaced, "new", key)
		delete(p.signatures, replaced) // one replacement per original
		p.markKnown(pl)
		if id, ok := espn.PlayID(pl); ok {
			p.advanceLast(id)
		}
	}
	for _, pl := range plays {
		p.signatures[espn.PlayKey(pl)] = playSignature(pl)
	}
	return kept
}

// endsPeriod is the play that closes a quarter, a half or the game.
func endsPeriod(pl espn.Data) bool {
	switch strings.ToLower(playType(pl)) {
	case "end period", "end of half", "end of game", "end of regulation":
		return true
	}
	return false
}

// dropAfterPeriodEnd takes out of unseen a football play from a period an
// earlier poll already saw end. Nothing from the 1st quarter can come after
// the end of the 1st, so a play that shows up late (ESPN re-posting a corrected
// play, or filling one in) is not announced. Plays that arrive in the same poll
// as the end of their period are sent as usual. The list is marked as handled.
func (p *Poller) dropAfterPeriodEnd(plays, unseen []espn.Data) []espn.Data {
	if p.ended == nil {
		p.ended = map[int64]bool{}
	}
	var kept []espn.Data
	for _, pl := range unseen {
		n, ok := espn.Int64(espn.Map(pl["period"])["number"])
		if !ok || !p.ended[n] || endsPeriod(pl) {
			kept = append(kept, pl)
			continue
		}
		slog.Debug("Play after the end of its period", "game", p.State.GameID, "id", espn.PlayKey(pl), "period", n,
			"clock", espn.Str(espn.Map(pl["clock"])["displayValue"]))
		p.markKnown(pl)
		if id, ok := espn.PlayID(pl); ok {
			p.advanceLast(id)
		}
	}
	for _, pl := range plays {
		if endsPeriod(pl) {
			if n, ok := espn.Int64(espn.Map(pl["period"])["number"]); ok {
				p.ended[n] = true
			}
		}
	}
	return kept
}

func (p *Poller) markKnown(pl espn.Data) {
	if p.known != nil {
		p.known[espn.PlayKey(pl)] = true
	}
}

// addLatePlays adds to unseen the football plays whose id is at or below
// last_play_id but that this run has not seen: ESPN inserted them late, or
// numbered them out of order. The result stays sorted by id.
func (p *Poller) addLatePlays(plays, unseen []espn.Data, last *PlayID) []espn.Data {
	if last == nil {
		if p.known == nil {
			p.known = map[string]bool{}
		}
		return unseen
	}
	if p.known == nil {
		// First poll of this run: whatever is at or below last_play_id was
		// handled before, here or in an earlier run.
		p.known = map[string]bool{}
		for _, pl := range plays {
			if id, ok := espn.PlayID(pl); ok && id <= int64(*last) {
				p.known[espn.PlayKey(pl)] = true
			}
		}
		return unseen
	}
	have := map[string]bool{}
	for _, pl := range unseen {
		have[espn.PlayKey(pl)] = true
	}
	added := false
	for _, pl := range plays {
		key := espn.PlayKey(pl)
		if id, ok := espn.PlayID(pl); ok && id <= int64(*last) && !p.known[key] && !have[key] {
			slog.Debug("Late play", "game", p.State.GameID, "id", key, "last_play_id", int64(*last))
			unseen = append(unseen, pl)
			added = true
		}
	}
	if added {
		sort.SliceStable(unseen, func(i, j int) bool {
			a, _ := espn.PlayID(unseen[i])
			b, _ := espn.PlayID(unseen[j])
			return a < b
		})
	}
	return unseen
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

// revisablePlay is true for the non-scoring plays worth re-sending when ESPN
// changes their text: basketball fouls and substitutions, NHL penalties and
// shots, baseball's "pitches to" plays and football plays.
// ESPN often posts these as a bare "foul" or "enters the game for X" and fills
// in the names a poll or two later. Other edits (a shot's description, an
// assist) are not worth a second message.
func revisablePlay(sport string, p espn.Data) bool {
	switch sport {
	case "nba", "wnba", "cbb":
		kind := strings.ToLower(playType(p))
		return kind == "substitution" || (strings.Contains(kind, "foul") && !strings.Contains(kind, "turnover"))
	case "nfl", "cfb":
		// ESPN corrects yardage, spots and names in a football play's text
		// for a few minutes ("enforced at BUF 24" became "BUF 25"). The row is
		// rewritten quietly; only recent plays, see findRevisions.
		return true
	case "mlb":
		// ESPN rewrites the pitcher in a "pitches to" play once a pitching
		// change is entered ("Yamamoto pitches to Mitchell" became "Vesia ...").
		return strings.Contains(strings.ToLower(playType(p)), "start batter")
	case "nhl":
		// Shots go out as a bare "Missed Shot" and are filled in with the
		// shooter, the shot and the goalie a poll or so later.
		switch strings.ToLower(playType(p)) {
		case "shot", "missed", "blocked":
			return true
		}
		return espn.Truthy(espn.Map(p["type"])["penaltyMinutes"])
	}
	return false
}

// staleUpdateAfter is how much older than the newest play a scoring play can be
// before a change to its text is only a quiet correction, not news. The
// numbers behind it (401 scoring-text changes over a weekend of games): the
// real additions, an extra point or an assist, arrived within 5 minutes
// (8 at the slowest), and the changes after that were rewordings.
const staleUpdateAfter = 5 * time.Minute

// periodMovedOnAfter is how old a play must be for a newer period to make it
// stale on its own, so an extra point added right after a quarter ends is
// still news.
const periodMovedOnAfter = time.Minute

// stalePlay is true when play is more than staleUpdateAfter older than the
// newest play in the feed (by their `wallclock` stamps), or is at least
// periodMovedOnAfter older and the newest play is in a later period (a later
// quarter, half or period; in baseball, a later half-inning). False if the
// stamps are missing. ESPN keeps rewording old scoring plays ("Edman to
// second on a throwing error", then back, an hour later); an extra point added
// to a touchdown arrives within a minute or two and still counts as an update.
func stalePlay(plays []espn.Data, play espn.Data) bool {
	at, err := time.Parse(time.RFC3339, espn.Str(play["wallclock"]))
	if err != nil {
		return false
	}
	var newest time.Time
	var latest espn.Data
	for _, pl := range plays {
		if t, err := time.Parse(time.RFC3339, espn.Str(pl["wallclock"])); err == nil && t.After(newest) {
			newest, latest = t, pl
		}
	}
	if newest.IsZero() {
		return false
	}
	age := newest.Sub(at)
	return age > staleUpdateAfter || (age >= periodMovedOnAfter && periodOf(play) != periodOf(latest))
}

// periodOf names a play's period: its number, and for baseball whether it is
// the top or the bottom.
func periodOf(p espn.Data) string {
	per := espn.Map(p["period"])
	return espn.Str(per["type"]) + "/" + espn.ScoreText(per["number"])
}

// tooOldToRevise is true for a football play that is stale (see stalePlay): there are hundreds of small corrections a game, and an
// old play's is not worth redrawing. Basketball, hockey and baseball
// revisions have no limit (they are few, and fill in a name that was missing).
func (p *Poller) tooOldToRevise(plays []espn.Data, pl espn.Data) bool {
	_, football := p.Rules.(footballRules)
	return football && stalePlay(plays, pl)
}

// findRevisions returns the revisable plays already sent whose text is now
// different, and that are not about to be sent as new plays.
func (p *Poller) findRevisions(plays []espn.Data, pending map[string]bool) []espn.Data {
	var out []espn.Data
	for _, pl := range plays {
		k := espn.PlayKey(pl)
		if old, ok := p.sentTexts[k]; ok && old != espn.PlayText(pl) && !pending[k] && revisablePlay(p.Sport, pl) && !p.tooOldToRevise(plays, pl) {
			out = append(out, pl)
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
