package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"
)

// The matching algorithm: turns an event's confirmed open entries into
// matches, each with a court, a time and a judge. See
// docs/design/events-and-matches.md, section 6. Matches already fixed (a
// challenge's, section 12) come in as taken court time and busy players and
// judges; they are never moved (rule 7).
//
// runMatcher is a pure function: no database, no clock, no randomness. The
// same input gives the same schedule whatever order its slices come in, and
// ties are broken by entry ID (user ID for judges, label for courts), so runs
// can be tested and compared.
//
// The steps, each below:
//
//  1. fixed teams from entries that named a partner;
//  2. one format for every entry or team that allows several;
//  3. pairing within each format (singles by neighbours, doubles and mixed
//     by teams);
//  4. slots: courts × time, in time order, keeping a gap between one
//     player's matches;
//  5. a judge for every match.

// ratingGapLimit is the widest gap between two sides the matcher accepts
// without flagging it for the admin (Proposed: about 200).
const ratingGapLimit = 200

// Match flags and standby reasons, as shown to admins.
const (
	FlagNoJudge         = "no judge"
	FlagGenderUnchecked = "gender balance not checked"

	StandbyNoOpponent = "no opponent"
	StandbyNoSlot     = "no free slot"
	StandbyNoFormat   = "format not offered"
)

// MatcherEntry is one confirmed open entry.
type MatcherEntry struct {
	ID, UserID string
	// Formats are the ones the player allowed; the entry gets one of them.
	Formats []string
	// PartnerUserID is the doubles or mixed partner named when joining, or "".
	PartnerUserID string
	CreatedAt     time.Time
	// Ratings is the player's rating per format; a missing format counts as
	// startingRating.
	Ratings map[string]int
}

func (e *MatcherEntry) rating(format string) int {
	if r, ok := e.Ratings[format]; ok {
		return r
	}
	return startingRating
}

// MatcherInput is everything the matcher looks at.
type MatcherInput struct {
	// Formats are what the event offers; SlotMinutes how long each takes.
	Formats     []string
	SlotMinutes map[string]int
	StartsAt    time.Time
	EndsAt      time.Time
	MaxMatches  int
	// Courts are filled "Court 1" first when two are free at the same time.
	Courts   []Court
	JudgeIDs []string
	Entries  []MatcherEntry
	// Fixed are matches already scheduled that the new ones must work
	// around: their court time is taken, and their players and judge are
	// busy. The event's own count towards MaxMatches.
	Fixed []FixedMatch
}

// FixedMatch is a match that already has its court and time: a scheduled
// challenge in this event, or another event's match on one of its courts.
type FixedMatch struct {
	// ID is the match's; fixture editing leaves a match out of its own way.
	ID               string
	CourtID          string
	StartsAt, EndsAt time.Time
	// UserIDs are its players; JudgeID is "" without a judge.
	UserIDs []string
	JudgeID string
	// OtherEvent is true for another event's match on a shared court: it
	// takes court time but not one of this event's max_matches.
	OtherEvent bool
}

// ownFixed is how many of the fixed matches are the event's own.
func ownFixed(fixed []FixedMatch) int {
	n := 0
	for _, f := range fixed {
		if !f.OtherEvent {
			n++
		}
	}
	return n
}

// ProposedPlayer is one player on a side, with the rating used to match them.
type ProposedPlayer struct {
	EntryID, UserID string
	Rating          int
}

// ProposedSide is one or two players. Rating is the average of theirs.
type ProposedSide struct {
	Players []ProposedPlayer
	Rating  int
}

// ProposedMatch is a match the matcher made. Sides[0] is side A, the higher
// rated (or equal).
type ProposedMatch struct {
	Format           string
	Sides            [2]ProposedSide
	CourtID          string
	StartsAt, EndsAt time.Time
	// JudgeID is "" when no judge was free (flagged FlagNoJudge).
	JudgeID string
	Flags   []string
}

// StandbyEntry is an entry the matcher couldn't place, and why.
type StandbyEntry struct {
	EntryID, UserID, Reason string
}

// Schedule is the matcher's output: matches in time order (then court),
// standby entries by entry ID, and warnings for the admin about the event as
// a whole.
type Schedule struct {
	Matches  []ProposedMatch
	Standby  []StandbyEntry
	Warnings []string
}

// unit is what gets a format: one solo entry, or a fixed team of two
// entries whose players joined together.
type unit struct {
	entries []*MatcherEntry // sorted by ID
	formats []string        // the formats it may play, in formatOrder
}

func (u unit) key() string { return u.entries[0].ID }

// lastJoined is the unit's latest entry: a team counts from its second
// player's join.
func (u unit) lastJoined() time.Time {
	t := u.entries[0].CreatedAt
	for _, e := range u.entries[1:] {
		if e.CreatedAt.After(t) {
			t = e.CreatedAt
		}
	}
	return t
}

// pending is a match after pairing, before it has a slot.
type pending struct {
	format string
	sides  [2]ProposedSide
	flags  []string
	// order is its place in pairing order, the final tie-break.
	order int
}

func (p pending) users() []string {
	var out []string
	for _, s := range p.sides {
		for _, pl := range s.Players {
			out = append(out, pl.UserID)
		}
	}
	return out
}

// matcher holds one run's working state.
type matcher struct {
	in       MatcherInput
	standby  []StandbyEntry
	warnings []string
}

func (m *matcher) bench(reason string, entries ...*MatcherEntry) {
	for _, e := range entries {
		m.standby = append(m.standby, StandbyEntry{EntryID: e.ID, UserID: e.UserID, Reason: reason})
	}
}

func (m *matcher) warn(format string, args ...any) {
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

// runMatcher builds the schedule for one event.
func runMatcher(in MatcherInput) Schedule {
	m := &matcher{in: in}

	// Work on a sorted copy so the caller's order never matters.
	entries := make([]*MatcherEntry, 0, len(in.Entries))
	for i := range in.Entries {
		e := in.Entries[i]
		var offered []string
		for _, f := range formatOrder {
			if slices.Contains(e.Formats, f) && slices.Contains(in.Formats, f) && in.SlotMinutes[f] > 0 {
				offered = append(offered, f)
			}
		}
		e.Formats = offered
		entries = append(entries, &e)
	}
	slices.SortFunc(entries, func(a, b *MatcherEntry) int { return cmp.Compare(a.ID, b.ID) })

	var usable []*MatcherEntry
	for _, e := range entries {
		if len(e.Formats) == 0 {
			m.bench(StandbyNoFormat, e)
			continue
		}
		usable = append(usable, e)
	}

	units := m.formTeams(usable)
	byFormat := assignFormats(units)

	var matches []pending
	for _, f := range formatOrder {
		switch f {
		case FormatSingles:
			matches = append(matches, m.pairSingles(byFormat[f])...)
		default:
			matches = append(matches, m.pairTeams(f, byFormat[f])...)
		}
	}
	for i := range matches {
		matches[i].order = i
	}

	out := m.schedule(matches)
	m.assignJudges(out)
	if n := ownFixed(in.Fixed); n > 0 {
		m.warn("%d %s", n, plural(n, "match is already scheduled and keeps its court and time",
			"matches are already scheduled and keep their courts and times"))
	}

	slices.SortFunc(m.standby, func(a, b StandbyEntry) int { return cmp.Compare(a.EntryID, b.EntryID) })
	if out == nil {
		out = []ProposedMatch{}
	}
	standby := m.standby
	if standby == nil {
		standby = []StandbyEntry{}
	}
	warnings := m.warnings
	if warnings == nil {
		warnings = []string{}
	}
	return Schedule{Matches: out, Standby: standby, Warnings: warnings}
}

// --- 1. fixed teams ---

// teamFormats is the doubles and mixed formats the entry allows.
func teamFormats(e *MatcherEntry) []string {
	var out []string
	for _, f := range e.Formats {
		if f == FormatDoubles || f == FormatMixed {
			out = append(out, f)
		}
	}
	return out
}

func intersect(a, b []string) []string {
	var out []string
	for _, f := range a {
		if slices.Contains(b, f) {
			out = append(out, f)
		}
	}
	return out
}

// formTeams keeps the partners who joined together (section 6, doubles 1).
// An entry naming a partner forms a fixed team with one of the partner's
// entries that shares a team format and isn't in another team yet.
//
// Conflicts are settled in two passes, each in entry-ID order:
//
//  1. mutual: both entries name each other;
//  2. one-sided: the partner's entry names no one. A partner's entry that
//     names someone else is left for that person.
//
// So when two entries name the same partner, a mutual request wins, then the
// lower entry ID. An entry whose partner can't be found plays solo and the
// matcher picks its team; the admin gets a warning.
//
// Every unit returned is sorted by key; solo units follow the same rule.
func (m *matcher) formTeams(entries []*MatcherEntry) []unit {
	byUser := map[string][]*MatcherEntry{}
	for _, e := range entries {
		byUser[e.UserID] = append(byUser[e.UserID], e)
	}
	used := map[string]bool{}
	var units []unit
	pair := func(e *MatcherEntry, accept func(p *MatcherEntry) bool) {
		for _, p := range byUser[e.PartnerUserID] {
			if used[p.ID] || !accept(p) {
				continue
			}
			common := intersect(teamFormats(e), teamFormats(p))
			if len(common) == 0 {
				continue
			}
			used[e.ID], used[p.ID] = true, true
			team := []*MatcherEntry{e, p}
			slices.SortFunc(team, func(a, b *MatcherEntry) int { return cmp.Compare(a.ID, b.ID) })
			units = append(units, unit{entries: team, formats: common})
			return
		}
	}
	for _, e := range entries {
		if e.PartnerUserID != "" && !used[e.ID] {
			pair(e, func(p *MatcherEntry) bool { return p.PartnerUserID == e.UserID })
		}
	}
	for _, e := range entries {
		if e.PartnerUserID != "" && !used[e.ID] {
			pair(e, func(p *MatcherEntry) bool { return p.PartnerUserID == "" })
		}
	}

	unmatched := 0
	for _, e := range entries {
		if used[e.ID] {
			continue
		}
		if e.PartnerUserID != "" {
			unmatched++
		}
		units = append(units, unit{entries: []*MatcherEntry{e}, formats: e.Formats})
	}
	if unmatched > 0 {
		m.warn("%d %s named a partner without a matching entry; the matcher picked the team instead",
			unmatched, plural(unmatched, "entry", "entries"))
	}
	slices.SortFunc(units, func(a, b unit) int { return cmp.Compare(a.key(), b.key()) })
	return units
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// --- 2. formats ---

// residues is a pool's head count where it matters for pairing: singles
// modulo 2 (two players a match), doubles and mixed modulo 4. Packed into one
// index, 2 × 4 × 4 = 32 states.
type residues int

func (r residues) add(format string, players int) residues {
	s, d, x := int(r)/16, int(r)/4%4, int(r)%4
	switch format {
	case FormatSingles:
		s = (s + players) % 2
	case FormatDoubles:
		d = (d + players) % 4
	case FormatMixed:
		x = (x + players) % 4
	}
	return residues(s*16 + d*4 + x)
}

// leftover is how many players can't be in a match with these residues: an
// odd singles player out, and doubles or mixed players short of a four.
func (r residues) leftover() int { return int(r)/16 + int(r)/4%4 + int(r)%4 }

// assignFormats gives every unit exactly one of its formats.
//
// The heuristic (Proposed; the design doc only says "whichever format pairs
// it best"): place as many entries as possible, counting heads only.
//
//   - A unit that allows one format plays it.
//   - The flexible ones (several formats) are then chosen so the fewest
//     players are left over: singles wants an even count, doubles and mixed
//     a multiple of four players (a fixed team counts as two). Only those
//     residues matter, so an exact dynamic programme over the 32 possible
//     residue states finds the best total in O(units × 32).
//   - Several choices are often equally good. Flexible units are then
//     settled in entry-ID order, each taking the first format in formatOrder
//     (singles, doubles, mixed) that still allows the best total. That keeps
//     it deterministic and easy to predict.
//
// Ratings play no part here; pairing within each format handles them.
// Constraints that only show up later (two entries of one player, slots)
// can still send an entry to standby.
func assignFormats(units []unit) map[string][]unit {
	byFormat := map[string][]unit{}
	var base residues
	var flexible []unit
	for _, u := range units {
		if len(u.formats) == 1 {
			byFormat[u.formats[0]] = append(byFormat[u.formats[0]], u)
			base = base.add(u.formats[0], len(u.entries))
		} else {
			flexible = append(flexible, u)
		}
	}

	// best[i][r] is the fewest players left over when units i.. are still to
	// be placed and the pools so far are at residues r.
	const states = 32
	best := make([][states]int, len(flexible)+1)
	for r := range states {
		best[len(flexible)][r] = residues(r).leftover()
	}
	for i := len(flexible) - 1; i >= 0; i-- {
		for r := range states {
			best[i][r] = math.MaxInt
			for _, f := range flexible[i].formats {
				best[i][r] = min(best[i][r], best[i+1][residues(r).add(f, len(flexible[i].entries))])
			}
		}
	}

	r := base
	for i, u := range flexible {
		for _, f := range u.formats {
			next := r.add(f, len(u.entries))
			if best[i+1][next] == best[i][r] {
				byFormat[f] = append(byFormat[f], u)
				r = next
				break
			}
		}
	}
	return byFormat
}

// --- 3. pairing ---

func gapFlags(sides [2]ProposedSide) []string {
	if gap := sides[0].Rating - sides[1].Rating; gap > ratingGapLimit || -gap > ratingGapLimit {
		return []string{fmt.Sprintf("rating gap %d", max(gap, -gap))}
	}
	return nil
}

// byRating sorts entries by their rating in format, highest first, then by
// entry ID.
func byRating(entries []*MatcherEntry, format string) {
	slices.SortFunc(entries, func(a, b *MatcherEntry) int {
		if c := cmp.Compare(b.rating(format), a.rating(format)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// latestFirst orders by join time, latest first, then by highest key: the
// order in which entries sit out when a pool has one too many.
func latestFirst(a, b unit) int {
	if c := b.lastJoined().Compare(a.lastJoined()); c != 0 {
		return c
	}
	return cmp.Compare(b.key(), a.key())
}

// pairSingles pairs neighbours by singles rating (section 6, singles).
//
// With an odd count, one entry sits out first: the one whose removal lets
// the most others be paired, and among those the one that joined last (paid
// entries are served in the order they came). Then, highest rated first,
// each player takes the next one down who isn't the same person (two entries
// of one player) and hasn't already been drawn against them in this run.
// Anyone left gets standby, "no opponent".
func (m *matcher) pairSingles(pool []unit) []pending {
	entries := make([]*MatcherEntry, len(pool))
	for i, u := range pool {
		entries[i] = u.entries[0]
	}
	byRating(entries, FormatSingles)

	if len(entries)%2 == 1 {
		order := slices.Clone(pool)
		slices.SortFunc(order, latestFirst)
		var out *MatcherEntry
		bestPairs := -1
		for _, u := range order {
			rest := slices.DeleteFunc(slices.Clone(entries), func(e *MatcherEntry) bool { return e == u.entries[0] })
			if pairs, _ := greedySingles(rest); len(pairs) > bestPairs {
				bestPairs, out = len(pairs), u.entries[0]
			}
		}
		m.bench(StandbyNoOpponent, out)
		entries = slices.DeleteFunc(entries, func(e *MatcherEntry) bool { return e == out })
	}

	pairs, unpaired := greedySingles(entries)
	m.bench(StandbyNoOpponent, unpaired...)
	matches := make([]pending, 0, len(pairs))
	for _, p := range pairs {
		var sides [2]ProposedSide
		for i, e := range p {
			r := e.rating(FormatSingles)
			sides[i] = ProposedSide{Players: []ProposedPlayer{{EntryID: e.ID, UserID: e.UserID, Rating: r}}, Rating: r}
		}
		matches = append(matches, pending{format: FormatSingles, sides: sides, flags: gapFlags(sides)})
	}
	return matches
}

// greedySingles pairs entries (already in rating order) as pairSingles
// describes.
//
// Greedy pairing can strand two entries that can't play each other (one
// player's two entries, say) when a different pairing would place them. So
// each stranded couple u, v then tries to break one existing pair p–q into
// u–p and v–q (or u–q and v–p), choosing the swap that adds the smallest
// rating gaps, earliest pair on a tie.
func greedySingles(entries []*MatcherEntry) (pairs [][2]*MatcherEntry, unpaired []*MatcherEntry) {
	met := map[[2]string]int{}
	meeting := func(a, b *MatcherEntry) [2]string {
		if a.UserID > b.UserID {
			a, b = b, a
		}
		return [2]string{a.UserID, b.UserID}
	}
	canPlay := func(a, b *MatcherEntry) bool { return a.UserID != b.UserID && met[meeting(a, b)] == 0 }
	taken := make([]bool, len(entries))
	for i, a := range entries {
		if taken[i] {
			continue
		}
		for j := i + 1; j < len(entries); j++ {
			b := entries[j]
			if taken[j] || !canPlay(a, b) {
				continue
			}
			taken[i], taken[j] = true, true
			met[meeting(a, b)]++
			pairs = append(pairs, [2]*MatcherEntry{a, b})
			break
		}
		if !taken[i] {
			unpaired = append(unpaired, a)
		}
	}

	gap := func(a, b *MatcherEntry) int {
		return max(a.rating(FormatSingles)-b.rating(FormatSingles), b.rating(FormatSingles)-a.rating(FormatSingles))
	}
	for repaired := true; repaired && len(unpaired) >= 2; {
		repaired = false
	stranded:
		for i, u := range unpaired {
			for _, v := range unpaired[i+1:] {
				best, bestCost := -1, 0
				var bestPairs [2][2]*MatcherEntry
				// After the greedy pass u and v can't play each other, or u
				// would have taken v.
				for k, pq := range pairs {
					met[meeting(pq[0], pq[1])]--
					for _, try := range [2][2][2]*MatcherEntry{
						{{u, pq[0]}, {v, pq[1]}},
						{{u, pq[1]}, {v, pq[0]}},
					} {
						if !canPlay(try[0][0], try[0][1]) || !canPlay(try[1][0], try[1][1]) {
							continue
						}
						if cost := gap(try[0][0], try[0][1]) + gap(try[1][0], try[1][1]); best < 0 || cost < bestCost {
							best, bestCost, bestPairs = k, cost, try
						}
					}
					met[meeting(pq[0], pq[1])]++
				}
				if best < 0 {
					continue
				}
				met[meeting(pairs[best][0], pairs[best][1])]--
				pairs = slices.Delete(pairs, best, best+1)
				for _, p := range bestPairs {
					// Higher rated (then lower ID) on side A, as in the greedy pass.
					if slices.Index(entries, p[1]) < slices.Index(entries, p[0]) {
						p[0], p[1] = p[1], p[0]
					}
					met[meeting(p[0], p[1])]++
					pairs = append(pairs, p)
				}
				unpaired = slices.DeleteFunc(unpaired, func(e *MatcherEntry) bool { return e == u || e == v })
				repaired = true
				break stranded
			}
		}
	}
	return pairs, unpaired
}

// team is two players on one side, with their entries.
type team struct {
	entries [2]*MatcherEntry
	rating  int
}

func newTeam(format string, a, b *MatcherEntry) team {
	// The average, rounded half up.
	return team{entries: [2]*MatcherEntry{a, b}, rating: (a.rating(format) + b.rating(format) + 1) / 2}
}

func (t team) has(userID string) bool {
	return t.entries[0].UserID == userID || t.entries[1].UserID == userID
}

// pairTeams makes doubles or mixed matches (section 6, doubles; mixed is
// paired the same way until profiles have a gender, and every mixed match is
// flagged so the admin checks it).
//
//  1. Fixed teams stay together.
//  2. Solo entries: with an odd count, the one that joined last sits out.
//     If the teams still can't pair off evenly, two more solos sit out (the
//     latest to join), or, with fewer than two solos, the fixed team that
//     completed last.
//  3. Solos make teams: sorted by rating, in groups of four (each group of
//     four different players), the highest teams with the lowest and the
//     middle two together. Two left over make one team.
//  4. All teams, sorted by team rating, pair with the next team down that
//     shares no player.
//
// Anyone left gets standby, "no opponent".
func (m *matcher) pairTeams(format string, pool []unit) []pending {
	var fixed []team
	var solos []unit
	for _, u := range pool {
		if len(u.entries) == 2 {
			fixed = append(fixed, newTeam(format, u.entries[0], u.entries[1]))
		} else {
			solos = append(solos, u)
		}
	}
	slices.SortFunc(solos, latestFirst)
	if len(solos)%2 == 1 {
		m.bench(StandbyNoOpponent, solos[0].entries[0])
		solos = solos[1:]
	}
	if (len(fixed)+len(solos)/2)%2 == 1 {
		if len(solos) >= 2 {
			m.bench(StandbyNoOpponent, solos[0].entries[0], solos[1].entries[0])
			solos = solos[2:]
		} else {
			slices.SortFunc(fixed, func(a, b team) int {
				return latestFirst(unit{entries: a.entries[:]}, unit{entries: b.entries[:]})
			})
			m.bench(StandbyNoOpponent, fixed[0].entries[0], fixed[0].entries[1])
			fixed = fixed[1:]
		}
	}

	rest := make([]*MatcherEntry, len(solos))
	for i, u := range solos {
		rest[i] = u.entries[0]
	}
	byRating(rest, format)
	teams := fixed
	for len(rest) >= 4 {
		group := []int{0}
		for j := 1; j < len(rest) && len(group) < 4; j++ {
			if !slices.ContainsFunc(group, func(k int) bool { return rest[k].UserID == rest[j].UserID }) {
				group = append(group, j)
			}
		}
		if len(group) < 4 {
			break
		}
		g := []*MatcherEntry{rest[group[0]], rest[group[1]], rest[group[2]], rest[group[3]]}
		teams = append(teams, newTeam(format, g[0], g[3]), newTeam(format, g[1], g[2]))
		for k := 3; k >= 0; k-- {
			rest = slices.Delete(rest, group[k], group[k]+1)
		}
	}
	if len(rest) == 2 && rest[0].UserID != rest[1].UserID {
		teams = append(teams, newTeam(format, rest[0], rest[1]))
		rest = nil
	}
	m.bench(StandbyNoOpponent, rest...)

	slices.SortFunc(teams, func(a, b team) int {
		if c := cmp.Compare(b.rating, a.rating); c != 0 {
			return c
		}
		return cmp.Compare(min(a.entries[0].ID, a.entries[1].ID), min(b.entries[0].ID, b.entries[1].ID))
	})
	taken := make([]bool, len(teams))
	var matches []pending
	for i, a := range teams {
		if taken[i] {
			continue
		}
		for j := i + 1; j < len(teams); j++ {
			b := teams[j]
			if taken[j] || b.has(a.entries[0].UserID) || b.has(a.entries[1].UserID) {
				continue
			}
			taken[i], taken[j] = true, true
			sides := [2]ProposedSide{teamSide(format, a), teamSide(format, b)}
			flags := gapFlags(sides)
			if format == FormatMixed {
				flags = append(flags, FlagGenderUnchecked)
			}
			matches = append(matches, pending{format: format, sides: sides, flags: flags})
			break
		}
		if !taken[i] {
			m.bench(StandbyNoOpponent, a.entries[0], a.entries[1])
		}
	}
	return matches
}

// teamSide lists a team's players highest rated first, then by entry ID.
func teamSide(format string, t team) ProposedSide {
	players := []*MatcherEntry{t.entries[0], t.entries[1]}
	byRating(players, format)
	side := ProposedSide{Rating: t.rating}
	for _, e := range players {
		side.Players = append(side.Players, ProposedPlayer{EntryID: e.ID, UserID: e.UserID, Rating: e.rating(format)})
	}
	return side
}

// --- 4. slots ---

// busy is a stretch of time someone or something is taken.
type busy struct {
	start, end time.Time
}

func (b busy) length() time.Duration { return b.end.Sub(b.start) }

// courtOrder sorts courts the way people read them, "Court 2" before
// "Court 10", then by ID.
func courtOrder(courts []Court) []Court {
	out := slices.Clone(courts)
	slices.SortFunc(out, func(a, b Court) int {
		if c := cmp.Compare(len(a.Label), len(b.Label)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Label, b.Label); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// schedule gives matches a court and a time, greedily: each match, in turn,
// takes the earliest start (then the first court in courtOrder) where
//
//   - the court is free for the whole slot, inside the event's window;
//   - none of its players has another match closer than the shorter of the
//     two slot lengths (section 6, Decided: never overlapping or
//     back-to-back, at least one slot between);
//   - the event's max_matches hasn't been reached, counting the event's
//     fixed matches.
//
// Fixed matches (MatcherInput.Fixed) are in place before the first turn:
// their court time is taken and their players are busy, with the same gap.
//
// The order matches take their turn in: matches whose players have the most
// matches go first (they are the hardest to fit), then doubles and mixed
// before singles (a team match places four entries per court slot, singles
// two), then pairing order. A match that fits nowhere sends its entries to
// standby, "no free slot".
//
// The earliest feasible start is always the event's start or the moment
// something stops being in the way (a match on the court ends, or a
// player's match ends plus the gap), so only those moments are tried.
func (m *matcher) schedule(matches []pending) []ProposedMatch {
	courts := courtOrder(m.in.Courts)
	matchesOf := map[string]int{}
	for _, p := range matches {
		for _, u := range p.users() {
			matchesOf[u]++
		}
	}
	busiest := func(p pending) int {
		most := 0
		for _, u := range p.users() {
			most = max(most, matchesOf[u])
		}
		return most
	}
	formatRank := map[string]int{FormatDoubles: 0, FormatMixed: 1, FormatSingles: 2}
	order := slices.Clone(matches)
	slices.SortStableFunc(order, func(a, b pending) int {
		if c := cmp.Compare(busiest(b), busiest(a)); c != 0 {
			return c
		}
		if c := cmp.Compare(formatRank[a.format], formatRank[b.format]); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})

	courtBusy := map[string][]busy{}
	playerBusy := map[string][]busy{}
	for _, f := range m.in.Fixed {
		b := busy{start: f.StartsAt, end: f.EndsAt}
		courtBusy[f.CourtID] = append(courtBusy[f.CourtID], b)
		for _, u := range f.UserIDs {
			playerBusy[u] = append(playerBusy[u], b)
		}
	}
	fixed := ownFixed(m.in.Fixed)
	var placed []ProposedMatch
	var capped, unfit int
	for _, p := range order {
		benchAll := func() {
			for _, s := range p.sides {
				for _, pl := range s.Players {
					m.standby = append(m.standby, StandbyEntry{EntryID: pl.EntryID, UserID: pl.UserID, Reason: StandbyNoSlot})
				}
			}
		}
		if fixed+len(placed) >= m.in.MaxMatches {
			capped++
			benchAll()
			continue
		}
		length := time.Duration(m.in.SlotMinutes[p.format]) * time.Minute
		users := p.users()

		candidates := []time.Time{m.in.StartsAt}
		for _, c := range courts {
			for _, b := range courtBusy[c.ID] {
				candidates = append(candidates, b.end)
			}
		}
		for _, u := range users {
			for _, b := range playerBusy[u] {
				candidates = append(candidates, b.end.Add(min(length, b.length())))
			}
		}
		slices.SortFunc(candidates, func(a, b time.Time) int { return a.Compare(b) })
		candidates = slices.CompactFunc(candidates, func(a, b time.Time) bool { return a.Equal(b) })

		var court string
		var start time.Time
	search:
		for _, t := range candidates {
			if t.Before(m.in.StartsAt) || t.Add(length).After(m.in.EndsAt) {
				continue
			}
			for _, u := range users {
				for _, b := range playerBusy[u] {
					gap := min(length, b.length())
					// Too close: the slots, each widened by the gap, overlap.
					if t.Before(b.end.Add(gap)) && t.Add(length).Add(gap).After(b.start) {
						continue search
					}
				}
			}
			for _, c := range courts {
				if !overlapsAny(courtBusy[c.ID], t, t.Add(length)) {
					court, start = c.ID, t
					break search
				}
			}
		}
		if court == "" {
			unfit++
			benchAll()
			continue
		}

		slot := busy{start: start, end: start.Add(length)}
		courtBusy[court] = append(courtBusy[court], slot)
		for _, u := range users {
			playerBusy[u] = append(playerBusy[u], slot)
		}
		flags := p.flags
		if flags == nil {
			flags = []string{}
		}
		placed = append(placed, ProposedMatch{
			Format: p.format, Sides: p.sides, CourtID: court,
			StartsAt: slot.start, EndsAt: slot.end, Flags: flags,
		})
	}

	if len(courts) == 0 && len(matches) > 0 {
		m.warn("the event has no courts, so no match has a slot")
	} else if unfit > 0 {
		m.warn("%d %s didn't fit on the courts in the event's time", unfit, plural(unfit, "match", "matches"))
	}
	if capped > 0 {
		m.warn("the event's limit of %d matches was reached; %d more %s couldn't be scheduled",
			m.in.MaxMatches, capped, plural(capped, "match", "matches"))
	}

	rank := map[string]int{}
	for i, c := range courts {
		rank[c.ID] = i
	}
	slices.SortFunc(placed, func(a, b ProposedMatch) int {
		if c := a.StartsAt.Compare(b.StartsAt); c != 0 {
			return c
		}
		return cmp.Compare(rank[a.CourtID], rank[b.CourtID])
	})
	return placed
}

// overlapsAny reports whether [start, end) overlaps any of the stretches.
func overlapsAny(stretches []busy, start, end time.Time) bool {
	for _, b := range stretches {
		if start.Before(b.end) && end.After(b.start) {
			return true
		}
	}
	return false
}

// --- 5. judges ---

// assignJudges gives each match, in time order, a judge from the event's
// list who isn't playing in it, isn't playing or judging another match at an
// overlapping time (fixed ones included), and has judged the fewest matches
// so far (fixed ones included, then lowest user ID), which spreads the load. A match with no such judge keeps an
// empty JudgeID and the flag FlagNoJudge for the admin (section 6).
func (m *matcher) assignJudges(matches []ProposedMatch) {
	judges := slices.Clone(m.in.JudgeIDs)
	slices.Sort(judges)
	judges = slices.Compact(judges)
	if len(judges) == 0 && len(matches) > 0 {
		m.warn("the event has no judges; add some so every match has one")
	}

	playing := map[string][]busy{}
	judging := map[string][]busy{}
	for _, f := range m.in.Fixed {
		for _, u := range f.UserIDs {
			playing[u] = append(playing[u], busy{f.StartsAt, f.EndsAt})
		}
		if f.JudgeID != "" {
			judging[f.JudgeID] = append(judging[f.JudgeID], busy{f.StartsAt, f.EndsAt})
		}
	}
	for _, mt := range matches {
		for _, s := range mt.Sides {
			for _, pl := range s.Players {
				playing[pl.UserID] = append(playing[pl.UserID], busy{mt.StartsAt, mt.EndsAt})
			}
		}
	}
	for i := range matches {
		mt := &matches[i]
		chosen := ""
		for _, j := range judges {
			if overlapsAny(playing[j], mt.StartsAt, mt.EndsAt) || overlapsAny(judging[j], mt.StartsAt, mt.EndsAt) {
				continue // playing in this match or another at the same time, or judging one
			}
			if chosen == "" || len(judging[j]) < len(judging[chosen]) {
				chosen = j
			}
		}
		if chosen == "" {
			mt.Flags = append(mt.Flags, FlagNoJudge)
			continue
		}
		mt.JudgeID = chosen
		judging[chosen] = append(judging[chosen], busy{mt.StartsAt, mt.EndsAt})
	}
}
