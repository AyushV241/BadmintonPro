package main

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The matcher is pure, so these tests need no database.

// matcherStart is 20:00 on a Wednesday in UTC; the zone doesn't matter here.
var matcherStart = time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)

// baseInput is an event on two courts for two hours, offering all three
// formats with the default slots, two judges, and no entries.
func baseInput() MatcherInput {
	return MatcherInput{
		Formats:     []string{FormatSingles, FormatDoubles, FormatMixed},
		SlotMinutes: map[string]int{FormatSingles: 30, FormatDoubles: 40, FormatMixed: 40},
		StartsAt:    matcherStart,
		EndsAt:      matcherStart.Add(2 * time.Hour),
		MaxMatches:  20,
		Courts:      []Court{{ID: "crt_1", Label: "Court 1"}, {ID: "crt_2", Label: "Court 2"}},
		JudgeIDs:    []string{"usr_judge_a", "usr_judge_b"},
	}
}

// entrySpec is a compact way to write an entry: "id user formats rating"
// plus an optional partner, joined minutes after the first entry.
type entrySpec struct {
	id, user string
	formats  string // "s", "d", "m" or any mix, e.g. "sd"
	rating   int
	partner  string
	joined   int // minutes
}

func entriesOf(specs ...entrySpec) []MatcherEntry {
	codes := map[rune]string{'s': FormatSingles, 'd': FormatDoubles, 'm': FormatMixed}
	var out []MatcherEntry
	for _, s := range specs {
		var formats []string
		for _, c := range s.formats {
			formats = append(formats, codes[c])
		}
		rating := s.rating
		if rating == 0 {
			rating = startingRating
		}
		out = append(out, MatcherEntry{
			ID: s.id, UserID: s.user, Formats: formats, PartnerUserID: s.partner,
			CreatedAt: matcherStart.Add(-48 * time.Hour).Add(time.Duration(s.joined) * time.Minute),
			Ratings:   map[string]int{FormatSingles: rating, FormatDoubles: rating, FormatMixed: rating},
		})
	}
	return out
}

// matchup describes a match by its entries: "e1 v e2" or "e1+e2 v e3+e4",
// each side in the matcher's player order.
func matchup(m ProposedMatch) string {
	var sides []string
	for _, s := range m.Sides {
		var ids []string
		for _, p := range s.Players {
			ids = append(ids, p.EntryID)
		}
		sides = append(sides, strings.Join(ids, "+"))
	}
	return strings.Join(sides, " v ")
}

func matchups(s Schedule) []string {
	var out []string
	for _, m := range s.Matches {
		out = append(out, matchup(m))
	}
	slices.Sort(out)
	return out
}

func standbyOf(s Schedule) map[string]string {
	out := map[string]string{}
	for _, e := range s.Standby {
		out[e.EntryID] = e.Reason
	}
	return out
}

func assertMatchups(t *testing.T, s Schedule, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := matchups(s); !slices.Equal(got, want) {
		t.Errorf("matches = %q, want %q", got, want)
	}
}

func assertStandby(t *testing.T, s Schedule, want map[string]string) {
	t.Helper()
	if got := standbyOf(s); !reflect.DeepEqual(got, want) {
		t.Errorf("standby = %v, want %v", got, want)
	}
}

func runChecked(t *testing.T, in MatcherInput) Schedule {
	t.Helper()
	s := runMatcher(in)
	checkInvariants(t, in, s)
	return s
}

type slot struct {
	start, end time.Time
	length     time.Duration
}

func overlaps(slots []slot, s slot) bool {
	for _, o := range slots {
		if s.start.Before(o.end) && s.end.After(o.start) {
			return true
		}
	}
	return false
}

// checkInvariants checks the rules every schedule must keep, whatever the
// input: every entry is in exactly one match or on standby, no player plays
// twice too close together or against themselves, courts and judges are
// never double-booked, everything fits the window and max_matches.
func checkInvariants(t *testing.T, in MatcherInput, s Schedule) {
	t.Helper()
	entryUser := map[string]string{}
	for _, e := range in.Entries {
		entryUser[e.ID] = e.UserID
	}
	seen := map[string]int{}
	for _, e := range s.Standby {
		seen[e.EntryID]++
	}
	if len(s.Matches) > in.MaxMatches {
		t.Errorf("%d matches, over max_matches %d", len(s.Matches), in.MaxMatches)
	}
	players := map[string][]slot{}
	courts := map[string][]slot{}
	judges := map[string][]slot{}
	validCourt := map[string]bool{}
	for _, c := range in.Courts {
		validCourt[c.ID] = true
	}
	for _, m := range s.Matches {
		want := time.Duration(in.SlotMinutes[m.Format]) * time.Minute
		if m.EndsAt.Sub(m.StartsAt) != want || m.StartsAt.Before(in.StartsAt) || m.EndsAt.After(in.EndsAt) {
			t.Errorf("%s: %v–%v doesn't fit a %v slot in the window", matchup(m), m.StartsAt, m.EndsAt, want)
		}
		if !validCourt[m.CourtID] {
			t.Errorf("%s: unknown court %q", matchup(m), m.CourtID)
		}
		size := 1
		if m.Format != FormatSingles {
			size = 2
		}
		inMatch := map[string]bool{}
		for _, side := range m.Sides {
			if len(side.Players) != size {
				t.Errorf("%s: a %s side has %d players", matchup(m), m.Format, len(side.Players))
			}
			for _, p := range side.Players {
				seen[p.EntryID]++
				if entryUser[p.EntryID] != p.UserID {
					t.Errorf("%s: entry %s isn't %s's", matchup(m), p.EntryID, p.UserID)
				}
				if inMatch[p.UserID] {
					t.Errorf("%s: %s appears twice", matchup(m), p.UserID)
				}
				inMatch[p.UserID] = true
			}
		}
		if inMatch[m.JudgeID] {
			t.Errorf("%s: judged by one of its players", matchup(m))
		}
		if (m.JudgeID == "") != slices.Contains(m.Flags, FlagNoJudge) {
			t.Errorf("%s: judge %q with flags %v", matchup(m), m.JudgeID, m.Flags)
		}
		here := slot{m.StartsAt, m.EndsAt, want}
		for u := range inMatch {
			for _, o := range players[u] {
				gap := min(here.length, o.length)
				if here.start.Before(o.end.Add(gap)) && here.end.Add(gap).After(o.start) {
					t.Errorf("%s plays at %v and %v, closer than %v", u, o.start, here.start, gap)
				}
			}
			players[u] = append(players[u], here)
		}
		if overlaps(courts[m.CourtID], here) {
			t.Errorf("court %s double-booked at %v", m.CourtID, here.start)
		}
		courts[m.CourtID] = append(courts[m.CourtID], here)
		if m.JudgeID != "" {
			if overlaps(judges[m.JudgeID], here) {
				t.Errorf("judge %s judges two matches at %v", m.JudgeID, here.start)
			}
			judges[m.JudgeID] = append(judges[m.JudgeID], here)
		}
	}
	// A judge never plays during a match they judge.
	for j, js := range judges {
		for _, a := range js {
			if overlaps(players[j], a) {
				t.Errorf("judge %s judges at %v while playing", j, a.start)
			}
		}
	}
	for id := range entryUser {
		if seen[id] != 1 {
			t.Errorf("entry %s appears %d times in matches and standby, want exactly once", id, seen[id])
		}
	}
	for i := 1; i < len(s.Matches); i++ {
		if s.Matches[i].StartsAt.Before(s.Matches[i-1].StartsAt) {
			t.Errorf("matches aren't in time order")
		}
	}
}

// --- singles ---

func TestSinglesPairsNeighbours(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s", rating: 1500},
		entrySpec{id: "ent_b", user: "b", formats: "s", rating: 1200},
		entrySpec{id: "ent_c", user: "c", formats: "s", rating: 1450},
		entrySpec{id: "ent_d", user: "d", formats: "s", rating: 1250},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_a v ent_c", "ent_d v ent_b")
	assertStandby(t, s, map[string]string{})
	for _, m := range s.Matches {
		if m.Sides[0].Rating < m.Sides[1].Rating {
			t.Errorf("%s: side A should be the higher rated", matchup(m))
		}
		if m.JudgeID == "" || len(m.Flags) != 0 {
			t.Errorf("%s: judge %q, flags %v", matchup(m), m.JudgeID, m.Flags)
		}
	}
	if len(s.Warnings) != 0 {
		t.Errorf("warnings = %v", s.Warnings)
	}
}

func TestSinglesOddOneOutIsTheLastToJoin(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s", rating: 1500, joined: 1},
		entrySpec{id: "ent_b", user: "b", formats: "s", rating: 1400, joined: 5}, // last
		entrySpec{id: "ent_c", user: "c", formats: "s", rating: 1300, joined: 2},
		entrySpec{id: "ent_d", user: "d", formats: "s", rating: 1200, joined: 3},
		entrySpec{id: "ent_e", user: "e", formats: "s", rating: 1100, joined: 4},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_a v ent_c", "ent_d v ent_e")
	assertStandby(t, s, map[string]string{"ent_b": StandbyNoOpponent})
}

func TestSinglesSameJoinTimeSitsOutHighestID(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s"},
		entrySpec{id: "ent_b", user: "b", formats: "s"},
		entrySpec{id: "ent_c", user: "c", formats: "s"},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_a v ent_b")
	assertStandby(t, s, map[string]string{"ent_c": StandbyNoOpponent})
}

func TestSinglesNeverPairsAPlayerWithThemselves(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_x2", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_y", user: "y", formats: "s", rating: 1450},
		entrySpec{id: "ent_z", user: "z", formats: "s", rating: 1400},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_x1 v ent_y", "ent_x2 v ent_z")
}

func TestSinglesOddOneOutAvoidsSelfPairing(t *testing.T) {
	// Benching y (who joined last) would leave x against x; benching x2
	// lets a match happen.
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "s", joined: 1},
		entrySpec{id: "ent_x2", user: "x", formats: "s", joined: 2},
		entrySpec{id: "ent_y", user: "y", formats: "s", joined: 3},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_x1 v ent_y")
	assertStandby(t, s, map[string]string{"ent_x2": StandbyNoOpponent})
}

func TestSinglesRepairsAStrandedPair(t *testing.T) {
	// Greedy pairing in ID order (equal ratings) would make a v b and leave
	// x against x; breaking a v b places everyone.
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s"},
		entrySpec{id: "ent_b", user: "b", formats: "s"},
		entrySpec{id: "ent_x1", user: "x", formats: "s"},
		entrySpec{id: "ent_x2", user: "x", formats: "s"},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_a v ent_x1", "ent_b v ent_x2")
	assertStandby(t, s, map[string]string{})
}

func TestSinglesOnlyOnePlayer(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "s"},
		entrySpec{id: "ent_x2", user: "x", formats: "s"},
	)
	s := runChecked(t, in)
	assertMatchups(t, s)
	assertStandby(t, s, map[string]string{"ent_x1": StandbyNoOpponent, "ent_x2": StandbyNoOpponent})
}

func TestSinglesNoRepeatMeeting(t *testing.T) {
	// x and y both have two entries and are closest in rating; they meet
	// once, and each plays someone else the second time.
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_x2", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_y1", user: "y", formats: "s", rating: 1490},
		entrySpec{id: "ent_y2", user: "y", formats: "s", rating: 1490},
		entrySpec{id: "ent_z", user: "z", formats: "s", rating: 1300},
		entrySpec{id: "ent_w", user: "w", formats: "s", rating: 1290},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_x1 v ent_y1", "ent_x2 v ent_z", "ent_y2 v ent_w")
	meetings := map[string]int{}
	for _, m := range s.Matches {
		pair := []string{m.Sides[0].Players[0].UserID, m.Sides[1].Players[0].UserID}
		slices.Sort(pair)
		meetings[strings.Join(pair, "-")]++
	}
	for pair, n := range meetings {
		if n > 1 {
			t.Errorf("%s meet %d times", pair, n)
		}
	}
}

func TestRatingGapFlag(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s", rating: 1700},
		entrySpec{id: "ent_b", user: "b", formats: "s", rating: 1400},
		entrySpec{id: "ent_c", user: "c", formats: "s", rating: 1300},
		entrySpec{id: "ent_d", user: "d", formats: "s", rating: 1100}, // exactly 200 below c
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_a v ent_b", "ent_c v ent_d")
	for _, m := range s.Matches {
		switch matchup(m) {
		case "ent_a v ent_b":
			if !slices.Equal(m.Flags, []string{"rating gap 300"}) {
				t.Errorf("a v b flags = %v", m.Flags)
			}
		case "ent_c v ent_d":
			if len(m.Flags) != 0 {
				t.Errorf("a gap of exactly 200 is fine: flags = %v", m.Flags)
			}
		}
	}
}

// --- doubles and mixed ---

func TestDoublesGroupsOfFour(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1600},
		entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1500},
		entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1400},
		entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1301},
	)
	s := runChecked(t, in)
	// Highest with lowest against the middle two.
	assertMatchups(t, s, "ent_a+ent_d v ent_b+ent_c")
	// a+d = (1600+1301)/2 = 1450.5, rounded half up to 1451, so it's side A.
	m := s.Matches[0]
	if m.Format != FormatDoubles {
		t.Errorf("format = %s", m.Format)
	}
	if m.Sides[0].Rating != 1451 || m.Sides[1].Rating != 1450 {
		t.Errorf("team ratings = %d, %d; want 1451 (a+d) and 1450 (b+c)", m.Sides[0].Rating, m.Sides[1].Rating)
	}
}

func TestDoublesTeamsPlayNeighbours(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		// Fixed teams: 1700 and 1300 average.
		entrySpec{id: "ent_f1", user: "f1", formats: "d", rating: 1700, partner: "f2"},
		entrySpec{id: "ent_f2", user: "f2", formats: "d", rating: 1700, partner: "f1"},
		entrySpec{id: "ent_g1", user: "g1", formats: "d", rating: 1300, partner: "g2"},
		entrySpec{id: "ent_g2", user: "g2", formats: "d", rating: 1300},
		// Solos making teams of 1680 and 1320.
		entrySpec{id: "ent_s1", user: "s1", formats: "d", rating: 1700},
		entrySpec{id: "ent_s2", user: "s2", formats: "d", rating: 1660},
		entrySpec{id: "ent_s3", user: "s3", formats: "d", rating: 1340},
		entrySpec{id: "ent_s4", user: "s4", formats: "d", rating: 1300},
	)
	s := runChecked(t, in)
	// Teams by rating: f (1700), s1+s4 (1500), s2+s3 (1500), g (1300).
	assertMatchups(t, s, "ent_f1+ent_f2 v ent_s1+ent_s4", "ent_s2+ent_s3 v ent_g1+ent_g2")
	for _, m := range s.Matches {
		if !slices.Equal(m.Flags, []string{"rating gap 200"}) && len(m.Flags) != 0 {
			t.Errorf("%s flags = %v", matchup(m), m.Flags)
		}
	}
}

func TestFixedPartners(t *testing.T) {
	cases := []struct {
		name    string
		entries []MatcherEntry
		want    []string
		standby map[string]string
		warned  bool
	}{{
		name: "mutual",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1800, partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1700, partner: "a"},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1600},
			entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1100},
		),
		// Left to itself the matcher would team a+d against b+c; the
		// partners stay together instead.
		want: []string{"ent_a+ent_b v ent_c+ent_d"},
	}, {
		name: "one-sided",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1800, partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1700},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1600},
			entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1100},
		),
		want: []string{"ent_a+ent_b v ent_c+ent_d"},
	}, {
		name: "two name the same partner: lower entry ID wins",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1800, partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1700},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1600, partner: "b"},
			entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1100},
		),
		want:   []string{"ent_a+ent_b v ent_c+ent_d"},
		warned: true,
	}, {
		name: "a mutual request beats a lower entry ID",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1800, partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1700, partner: "c"},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1600, partner: "b"},
			entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1100},
		),
		want:   []string{"ent_b+ent_c v ent_a+ent_d"},
		warned: true,
	}, {
		name: "a partner's entry naming someone else stays theirs",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1800, partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1700, partner: "d"},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1600},
			entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1100},
		),
		want:   []string{"ent_a+ent_c v ent_b+ent_d"},
		warned: true,
	}, {
		name: "no shared format",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1800, partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "m", rating: 1700},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1600},
			entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1100},
			entrySpec{id: "ent_e", user: "e", formats: "d", rating: 1000},
		),
		// a plays solo; b is alone in mixed.
		want:    []string{"ent_a+ent_e v ent_c+ent_d"},
		standby: map[string]string{"ent_b": StandbyNoOpponent},
		warned:  true,
	}, {
		name: "partner not in the event",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", partner: "ghost"},
			entrySpec{id: "ent_b", user: "b", formats: "d"},
			entrySpec{id: "ent_c", user: "c", formats: "d"},
			entrySpec{id: "ent_d", user: "d", formats: "d"},
		),
		want:   []string{"ent_a+ent_d v ent_b+ent_c"},
		warned: true,
	}, {
		name: "a team never plays itself: a partner holding a second entry",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1500, partner: "b"},
			entrySpec{id: "ent_b1", user: "b", formats: "d", rating: 1500, partner: "a"},
			entrySpec{id: "ent_b2", user: "b", formats: "d", rating: 1500},
			entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1500},
		),
		// b2+c can't face a+b1: b would be on both sides.
		want: nil,
		standby: map[string]string{
			"ent_a": StandbyNoOpponent, "ent_b1": StandbyNoOpponent,
			"ent_b2": StandbyNoOpponent, "ent_c": StandbyNoOpponent,
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			in.Entries = tc.entries
			s := runChecked(t, in)
			assertMatchups(t, s, tc.want...)
			if tc.standby == nil {
				tc.standby = map[string]string{}
			}
			assertStandby(t, s, tc.standby)
			if warned := slices.ContainsFunc(s.Warnings, func(w string) bool { return strings.Contains(w, "partner") }); warned != tc.warned {
				t.Errorf("warnings = %v, want a partner warning: %v", s.Warnings, tc.warned)
			}
		})
	}
}

func TestDoublesSoloPoolKeepsOnePlayerOffBothSides(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "d", rating: 1600},
		entrySpec{id: "ent_x2", user: "x", formats: "d", rating: 1600},
		entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1500},
		entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1400},
		entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1300},
		entrySpec{id: "ent_d", user: "d", formats: "d", rating: 1200},
		entrySpec{id: "ent_e", user: "e", formats: "d", rating: 1100},
		entrySpec{id: "ent_f", user: "f", formats: "d", rating: 1000},
	)
	s := runChecked(t, in) // the invariants check nobody appears twice in a match
	if len(s.Matches) != 2 || len(s.Standby) != 0 {
		t.Errorf("matches = %v, standby = %v", matchups(s), s.Standby)
	}
}

func TestDoublesLeftovers(t *testing.T) {
	cases := []struct {
		name    string
		entries []MatcherEntry
		standby []string
	}{{
		name: "five solos: the last to join sits out",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", joined: 5},
			entrySpec{id: "ent_b", user: "b", formats: "d", joined: 1},
			entrySpec{id: "ent_c", user: "c", formats: "d", joined: 2},
			entrySpec{id: "ent_d", user: "d", formats: "d", joined: 3},
			entrySpec{id: "ent_e", user: "e", formats: "d", joined: 4},
		),
		standby: []string{"ent_a"},
	}, {
		name: "six solos: the last two sit out",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", joined: 6},
			entrySpec{id: "ent_b", user: "b", formats: "d", joined: 5},
			entrySpec{id: "ent_c", user: "c", formats: "d", joined: 1},
			entrySpec{id: "ent_d", user: "d", formats: "d", joined: 2},
			entrySpec{id: "ent_e", user: "e", formats: "d", joined: 3},
			entrySpec{id: "ent_f", user: "f", formats: "d", joined: 4},
		),
		standby: []string{"ent_a", "ent_b"},
	}, {
		name: "a team and two solos: solos sit out before a team",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", partner: "b", joined: 9},
			entrySpec{id: "ent_b", user: "b", formats: "d", joined: 9},
			entrySpec{id: "ent_c", user: "c", formats: "d", partner: "d"},
			entrySpec{id: "ent_d", user: "d", formats: "d"},
			entrySpec{id: "ent_e", user: "e", formats: "d", partner: "f"},
			entrySpec{id: "ent_f", user: "f", formats: "d"},
			entrySpec{id: "ent_g", user: "g", formats: "d", joined: 1},
			entrySpec{id: "ent_h", user: "h", formats: "d", joined: 2},
		),
		// 3 fixed teams + 1 solo team = 4 teams: nobody sits out.
		standby: nil,
	}, {
		name: "three fixed teams: the last completed sits out",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "d", partner: "b", joined: 1},
			entrySpec{id: "ent_b", user: "b", formats: "d", joined: 8},
			entrySpec{id: "ent_c", user: "c", formats: "d", partner: "d", joined: 2},
			entrySpec{id: "ent_d", user: "d", formats: "d", joined: 3},
			entrySpec{id: "ent_e", user: "e", formats: "d", partner: "f", joined: 4},
			entrySpec{id: "ent_f", user: "f", formats: "d", joined: 5},
		),
		standby: []string{"ent_a", "ent_b"},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			in.Entries = tc.entries
			s := runChecked(t, in)
			want := map[string]string{}
			for _, id := range tc.standby {
				want[id] = StandbyNoOpponent
			}
			assertStandby(t, s, want)
		})
	}
}

func TestMixedIsPairedLikeDoublesAndFlagged(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "m", rating: 1600},
		entrySpec{id: "ent_b", user: "b", formats: "m", rating: 1500},
		entrySpec{id: "ent_c", user: "c", formats: "m", rating: 1400},
		entrySpec{id: "ent_d", user: "d", formats: "m", rating: 1300},
	)
	s := runChecked(t, in)
	assertMatchups(t, s, "ent_a+ent_d v ent_b+ent_c")
	if m := s.Matches[0]; m.Format != FormatMixed || !slices.Contains(m.Flags, FlagGenderUnchecked) {
		t.Errorf("mixed match = %+v", m)
	}
}

// --- formats ---

func TestFlexibleEntriesFillFormats(t *testing.T) {
	cases := []struct {
		name    string
		entries []MatcherEntry
		formats map[string]string // entry → format it played
		standby []string
	}{{
		name: "a flexible entry completes the doubles four",
		entries: entriesOf(
			entrySpec{id: "ent_s1", user: "s1", formats: "s"},
			entrySpec{id: "ent_s2", user: "s2", formats: "s"},
			entrySpec{id: "ent_s3", user: "s3", formats: "s"},
			entrySpec{id: "ent_d1", user: "d1", formats: "d"},
			entrySpec{id: "ent_d2", user: "d2", formats: "d"},
			entrySpec{id: "ent_d3", user: "d3", formats: "d"},
			entrySpec{id: "ent_f", user: "f", formats: "sd"},
		),
		formats: map[string]string{"ent_f": FormatDoubles},
		standby: []string{"ent_s3"}, // three singles: the last to join (highest ID on a tie) sits out
	}, {
		name: "a flexible entry evens out singles",
		entries: entriesOf(
			entrySpec{id: "ent_s1", user: "s1", formats: "s"},
			entrySpec{id: "ent_d1", user: "d1", formats: "d"},
			entrySpec{id: "ent_d2", user: "d2", formats: "d"},
			entrySpec{id: "ent_d3", user: "d3", formats: "d"},
			entrySpec{id: "ent_d4", user: "d4", formats: "d"},
			entrySpec{id: "ent_f", user: "f", formats: "sd"},
		),
		formats: map[string]string{"ent_f": FormatSingles},
	}, {
		name: "equally good: singles first, in entry-ID order",
		entries: entriesOf(
			entrySpec{id: "ent_f1", user: "f1", formats: "sd"},
			entrySpec{id: "ent_f2", user: "f2", formats: "sd"},
			entrySpec{id: "ent_f3", user: "f3", formats: "sd"},
			entrySpec{id: "ent_f4", user: "f4", formats: "sd"},
		),
		formats: map[string]string{"ent_f1": FormatSingles, "ent_f2": FormatSingles, "ent_f3": FormatSingles, "ent_f4": FormatSingles},
	}, {
		name: "flexible entries split between three formats",
		entries: entriesOf(
			entrySpec{id: "ent_s1", user: "s1", formats: "s"},
			entrySpec{id: "ent_d1", user: "d1", formats: "d"},
			entrySpec{id: "ent_d2", user: "d2", formats: "d"},
			entrySpec{id: "ent_m1", user: "m1", formats: "m"},
			entrySpec{id: "ent_m2", user: "m2", formats: "m"},
			entrySpec{id: "ent_m3", user: "m3", formats: "m"},
			entrySpec{id: "ent_f1", user: "f1", formats: "sdm"},
			entrySpec{id: "ent_f2", user: "f2", formats: "dm"},
			entrySpec{id: "ent_f3", user: "f3", formats: "dm"},
			entrySpec{id: "ent_f4", user: "f4", formats: "sdm"},
		),
		// s needs 1, d needs 2, m needs 1: f1 → singles, f2, f3 → doubles, f4 → mixed.
		formats: map[string]string{"ent_f1": FormatSingles, "ent_f2": FormatDoubles, "ent_f3": FormatDoubles, "ent_f4": FormatMixed},
	}, {
		name: "a flexible fixed team",
		entries: entriesOf(
			entrySpec{id: "ent_a", user: "a", formats: "dm", partner: "b"},
			entrySpec{id: "ent_b", user: "b", formats: "sdm", partner: "a"},
			entrySpec{id: "ent_m1", user: "m1", formats: "m"},
			entrySpec{id: "ent_m2", user: "m2", formats: "m"},
		),
		formats: map[string]string{"ent_a": FormatMixed, "ent_b": FormatMixed},
	}, {
		name: "a format the event doesn't offer",
		entries: entriesOf(
			entrySpec{id: "ent_s1", user: "s1", formats: "s"},
			entrySpec{id: "ent_s2", user: "s2", formats: "s"},
			entrySpec{id: "ent_m", user: "m", formats: "m"},
			entrySpec{id: "ent_f", user: "f", formats: "sm"},
		),
		// f can only play singles here, making three: the highest ID sits out.
		formats: map[string]string{"ent_s1": FormatSingles, "ent_f": FormatSingles},
		standby: []string{"ent_m", "ent_s2"},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			if tc.name == "a format the event doesn't offer" {
				in.Formats = []string{FormatSingles, FormatDoubles}
			}
			in.Entries = tc.entries
			s := runChecked(t, in)
			played := map[string]string{}
			for _, m := range s.Matches {
				for _, side := range m.Sides {
					for _, p := range side.Players {
						played[p.EntryID] = m.Format
					}
				}
			}
			for id, want := range tc.formats {
				if played[id] != want {
					t.Errorf("%s played %q, want %q (matches %v)", id, played[id], want, matchups(s))
				}
			}
			got := standbyOf(s)
			if len(got) != len(tc.standby) {
				t.Errorf("standby = %v, want %v", got, tc.standby)
			}
			for _, id := range tc.standby {
				if _, ok := got[id]; !ok {
					t.Errorf("standby = %v, want %s on it", got, id)
				}
			}
		})
	}
	// Not offered is its own reason.
	in := baseInput()
	in.Formats = []string{FormatSingles}
	in.Entries = entriesOf(entrySpec{id: "ent_m", user: "m", formats: "m"})
	assertStandby(t, runChecked(t, in), map[string]string{"ent_m": StandbyNoFormat})
}

func TestAssignFormatsMinimisesLeftovers(t *testing.T) {
	// Every mix of up to 7 flexible {s,d} entries alongside a few fixed
	// ones: the DP's head count matches brute force.
	for fixedS := range 3 {
		for fixedD := range 4 {
			for flex := range 8 {
				var units []unit
				id := 0
				add := func(formats ...string) {
					id++
					e := &MatcherEntry{ID: fmt.Sprintf("ent_%02d", id), UserID: fmt.Sprintf("u%d", id), Formats: formats}
					units = append(units, unit{entries: []*MatcherEntry{e}, formats: formats})
				}
				for range fixedS {
					add(FormatSingles)
				}
				for range fixedD {
					add(FormatDoubles)
				}
				for range flex {
					add(FormatSingles, FormatDoubles)
				}
				byFormat := assignFormats(units)
				s, d := len(byFormat[FormatSingles]), len(byFormat[FormatDoubles])
				got := s%2 + d%4
				want := 99
				for toS := 0; toS <= flex; toS++ {
					want = min(want, (fixedS+toS)%2+(fixedD+flex-toS)%4)
				}
				if s+d != len(units) || got != want {
					t.Errorf("fixed %d/%d, flexible %d: %d singles, %d doubles leave %d, best is %d", fixedS, fixedD, flex, s, d, got, want)
				}
			}
		}
	}
}

// --- slots ---

func TestSlotsPackCourtsInTimeOrder(t *testing.T) {
	in := baseInput()
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}}
	var specs []entrySpec
	for i := range 10 {
		specs = append(specs, entrySpec{id: fmt.Sprintf("ent_%02d", i), user: fmt.Sprintf("u%02d", i), formats: "s", rating: 1500 - i})
	}
	in.Entries = entriesOf(specs...)
	s := runChecked(t, in)
	// One court, two hours, 30-minute slots: four matches, back to back.
	if len(s.Matches) != 4 {
		t.Fatalf("matches = %v", matchups(s))
	}
	for i, m := range s.Matches {
		if want := matcherStart.Add(time.Duration(30*i) * time.Minute); !m.StartsAt.Equal(want) || m.CourtID != "crt_1" {
			t.Errorf("match %d at %v on %s, want %v", i, m.StartsAt, m.CourtID, want)
		}
	}
	// Strongest pairs first; the last pair doesn't fit.
	assertStandby(t, s, map[string]string{"ent_08": StandbyNoSlot, "ent_09": StandbyNoSlot})
	if !slices.ContainsFunc(s.Warnings, func(w string) bool { return strings.Contains(w, "didn't fit") }) {
		t.Errorf("warnings = %v", s.Warnings)
	}
}

func TestSlotsFillCourtsInLabelOrder(t *testing.T) {
	in := baseInput()
	in.Courts = []Court{{ID: "crt_z", Label: "Court 10"}, {ID: "crt_y", Label: "Court 2"}}
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s", rating: 1500},
		entrySpec{id: "ent_b", user: "b", formats: "s", rating: 1400},
		entrySpec{id: "ent_c", user: "c", formats: "s", rating: 1300},
		entrySpec{id: "ent_d", user: "d", formats: "s", rating: 1200},
	)
	s := runChecked(t, in)
	if len(s.Matches) != 2 || s.Matches[0].CourtID != "crt_y" || s.Matches[1].CourtID != "crt_z" ||
		!s.Matches[0].StartsAt.Equal(matcherStart) || !s.Matches[1].StartsAt.Equal(matcherStart) {
		t.Errorf("matches = %+v", s.Matches)
	}
}

func TestSlotsKeepAGapBetweenOnePlayersMatches(t *testing.T) {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_x2", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_y", user: "y", formats: "s", rating: 1490},
		entrySpec{id: "ent_z", user: "z", formats: "s", rating: 1480},
	)
	s := runChecked(t, in)
	if len(s.Matches) != 2 {
		t.Fatalf("matches = %v", matchups(s))
	}
	// Two courts are free at 20:00, but x needs a 30-minute break.
	if !s.Matches[0].StartsAt.Equal(matcherStart) || !s.Matches[1].StartsAt.Equal(matcherStart.Add(time.Hour)) {
		t.Errorf("x plays at %v and %v, want 20:00 and 21:00", s.Matches[0].StartsAt, s.Matches[1].StartsAt)
	}
}

func TestSlotsGapUsesTheShorterSlot(t *testing.T) {
	// x plays doubles (40) and singles (30): the gap is 30 minutes.
	in := baseInput()
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}, {ID: "crt_2", Label: "Court 2"}, {ID: "crt_3", Label: "Court 3"}}
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "d", rating: 1500},
		entrySpec{id: "ent_a", user: "a", formats: "d", rating: 1500},
		entrySpec{id: "ent_b", user: "b", formats: "d", rating: 1500},
		entrySpec{id: "ent_c", user: "c", formats: "d", rating: 1500},
		entrySpec{id: "ent_x2", user: "x", formats: "s", rating: 1500},
		entrySpec{id: "ent_y", user: "y", formats: "s", rating: 1500},
	)
	s := runChecked(t, in)
	var doubles, singles ProposedMatch
	for _, m := range s.Matches {
		if m.Format == FormatDoubles {
			doubles = m
		} else {
			singles = m
		}
	}
	// Doubles go first; singles can't start until 20:40 + 30.
	if !doubles.StartsAt.Equal(matcherStart) || !singles.StartsAt.Equal(matcherStart.Add(70*time.Minute)) {
		t.Errorf("doubles at %v, singles at %v", doubles.StartsAt, singles.StartsAt)
	}
}

func TestSlotsBusiestPlayersGoFirst(t *testing.T) {
	// One court, four slots. a has two matches; placed first they fit at
	// 20:00 and 21:00 with the others around them. In pairing order a's
	// second match would have nowhere to go.
	in := baseInput()
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}}
	in.Entries = entriesOf(
		entrySpec{id: "ent_d", user: "d", formats: "s", rating: 1900},
		entrySpec{id: "ent_e", user: "e", formats: "s", rating: 1890},
		entrySpec{id: "ent_f", user: "f", formats: "s", rating: 1800},
		entrySpec{id: "ent_g", user: "g", formats: "s", rating: 1790},
		entrySpec{id: "ent_a1", user: "a", formats: "s", rating: 1500},
		entrySpec{id: "ent_a2", user: "a", formats: "s", rating: 1500},
		entrySpec{id: "ent_b", user: "b", formats: "s", rating: 1490},
		entrySpec{id: "ent_c", user: "c", formats: "s", rating: 1480},
	)
	s := runChecked(t, in)
	if len(s.Matches) != 4 || len(s.Standby) != 0 {
		t.Fatalf("matches = %v, standby = %v", matchups(s), s.Standby)
	}
}

func TestSlotsThirdEntryDoesntFit(t *testing.T) {
	// Two hours: a player's matches at 20:00 and 21:00 leave no room for a
	// third with a break either side.
	in := baseInput()
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}, {ID: "crt_2", Label: "Court 2"}, {ID: "crt_3", Label: "Court 3"}}
	in.Entries = entriesOf(
		entrySpec{id: "ent_x1", user: "x", formats: "s"},
		entrySpec{id: "ent_x2", user: "x", formats: "s"},
		entrySpec{id: "ent_x3", user: "x", formats: "s"},
		entrySpec{id: "ent_a", user: "a", formats: "s"},
		entrySpec{id: "ent_b", user: "b", formats: "s"},
		entrySpec{id: "ent_c", user: "c", formats: "s"},
	)
	s := runChecked(t, in)
	if len(s.Matches) != 2 {
		t.Errorf("matches = %v", matchups(s))
	}
	got := standbyOf(s)
	if len(got) != 2 || got["ent_x3"] != StandbyNoSlot {
		t.Errorf("standby = %v", got)
	}
}

func TestMaxMatchesCap(t *testing.T) {
	in := baseInput()
	in.MaxMatches = 2
	var specs []entrySpec
	for i := range 6 {
		specs = append(specs, entrySpec{id: fmt.Sprintf("ent_%d", i), user: fmt.Sprintf("u%d", i), formats: "s", rating: 1500 - 10*i})
	}
	in.Entries = entriesOf(specs...)
	s := runChecked(t, in)
	if len(s.Matches) != 2 {
		t.Errorf("matches = %v", matchups(s))
	}
	assertStandby(t, s, map[string]string{"ent_4": StandbyNoSlot, "ent_5": StandbyNoSlot})
	if !slices.ContainsFunc(s.Warnings, func(w string) bool { return strings.Contains(w, "limit of 2 matches") }) {
		t.Errorf("warnings = %v", s.Warnings)
	}
}

func TestNoCourts(t *testing.T) {
	in := baseInput()
	in.Courts = nil
	in.Entries = entriesOf(entrySpec{id: "ent_a", user: "a", formats: "s"}, entrySpec{id: "ent_b", user: "b", formats: "s"})
	s := runChecked(t, in)
	assertStandby(t, s, map[string]string{"ent_a": StandbyNoSlot, "ent_b": StandbyNoSlot})
	if !slices.ContainsFunc(s.Warnings, func(w string) bool { return strings.Contains(w, "no courts") }) {
		t.Errorf("warnings = %v", s.Warnings)
	}
}

// --- judges ---

func fourSinglesOnTwoCourts() MatcherInput {
	in := baseInput()
	in.Entries = entriesOf(
		entrySpec{id: "ent_a", user: "a", formats: "s", rating: 1500},
		entrySpec{id: "ent_b", user: "b", formats: "s", rating: 1490},
		entrySpec{id: "ent_c", user: "c", formats: "s", rating: 1300},
		entrySpec{id: "ent_d", user: "d", formats: "s", rating: 1290},
	)
	return in
}

func TestNoJudgeFlag(t *testing.T) {
	in := fourSinglesOnTwoCourts()
	in.JudgeIDs = nil
	s := runChecked(t, in)
	for _, m := range s.Matches {
		if m.JudgeID != "" || !slices.Equal(m.Flags, []string{FlagNoJudge}) {
			t.Errorf("%s: judge %q, flags %v", matchup(m), m.JudgeID, m.Flags)
		}
	}
	if !slices.ContainsFunc(s.Warnings, func(w string) bool { return strings.Contains(w, "no judges") }) {
		t.Errorf("warnings = %v", s.Warnings)
	}

	// One judge, two matches at the same time: one goes without.
	in.JudgeIDs = []string{"usr_judge_a"}
	s = runChecked(t, in)
	if s.Matches[0].JudgeID != "usr_judge_a" || s.Matches[1].JudgeID != "" || !slices.Contains(s.Matches[1].Flags, FlagNoJudge) {
		t.Errorf("matches = %+v", s.Matches)
	}
}

func TestJudgeNeverJudgesTheirOwnMatch(t *testing.T) {
	// a is also the event's only judge: a's own match gets none, the other
	// match (at the same time) can't have a either.
	in := fourSinglesOnTwoCourts()
	in.JudgeIDs = []string{"a"}
	s := runChecked(t, in)
	for _, m := range s.Matches {
		if m.JudgeID == "a" {
			t.Errorf("%s judged by a while a plays at the same time", matchup(m))
		}
	}

	// With one court the matches are sequential: a judges the other one.
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}}
	s = runChecked(t, in)
	judged := map[string]string{}
	for _, m := range s.Matches {
		judged[matchup(m)] = m.JudgeID
	}
	if judged["ent_a v ent_b"] != "" || judged["ent_c v ent_d"] != "a" {
		t.Errorf("judges = %v", judged)
	}
}

func TestJudgesShareTheLoad(t *testing.T) {
	in := baseInput()
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}}
	in.JudgeIDs = []string{"usr_judge_b", "usr_judge_a", "usr_judge_c"}
	var specs []entrySpec
	for i := range 8 {
		specs = append(specs, entrySpec{id: fmt.Sprintf("ent_%d", i), user: fmt.Sprintf("u%d", i), formats: "s", rating: 1500 - 10*i})
	}
	in.Entries = entriesOf(specs...)
	s := runChecked(t, in)
	var got []string
	for _, m := range s.Matches {
		got = append(got, m.JudgeID)
	}
	// Fewest so far, then lowest ID.
	want := []string{"usr_judge_a", "usr_judge_b", "usr_judge_c", "usr_judge_a"}
	if !slices.Equal(got, want) {
		t.Errorf("judges = %v, want %v", got, want)
	}
}

// --- determinism and properties ---

// randomInput is a busy event: several formats, players with several
// entries, partners (some broken), spread ratings.
func randomInput(r *rand.Rand) MatcherInput {
	in := baseInput()
	in.Courts = []Court{{ID: "crt_1", Label: "Court 1"}, {ID: "crt_2", Label: "Court 2"}, {ID: "crt_3", Label: "Court 3"}}
	in.EndsAt = in.StartsAt.Add(3 * time.Hour)
	in.MaxMatches = 15 + r.IntN(10)
	in.JudgeIDs = []string{"usr_judge_a", "usr_judge_b", "u03"}
	choices := []string{"s", "d", "m", "sd", "dm", "sdm", "s", "d"}
	var specs []entrySpec
	players := 12 + r.IntN(20)
	for i := range players {
		user := fmt.Sprintf("u%02d", i)
		for k := range 1 + r.IntN(3)*r.IntN(2) {
			spec := entrySpec{
				id:      fmt.Sprintf("ent_%02d_%d", i, k),
				user:    user,
				formats: choices[r.IntN(len(choices))],
				rating:  1100 + r.IntN(700),
				joined:  r.IntN(60),
			}
			if strings.ContainsAny(spec.formats, "dm") && r.IntN(4) == 0 {
				spec.partner = fmt.Sprintf("u%02d", r.IntN(players+3)) // sometimes nobody
				if spec.partner == user {
					spec.partner = ""
				}
			}
			specs = append(specs, spec)
		}
	}
	in.Entries = entriesOf(specs...)
	return in
}

func TestMatcherIsDeterministic(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for round := range 50 {
		in := randomInput(r)
		want := runChecked(t, in)
		for range 5 {
			shuffled := in
			shuffled.Entries = slices.Clone(in.Entries)
			shuffled.Courts = slices.Clone(in.Courts)
			shuffled.JudgeIDs = slices.Clone(in.JudgeIDs)
			r.Shuffle(len(shuffled.Entries), func(i, j int) {
				shuffled.Entries[i], shuffled.Entries[j] = shuffled.Entries[j], shuffled.Entries[i]
			})
			r.Shuffle(len(shuffled.Courts), func(i, j int) { shuffled.Courts[i], shuffled.Courts[j] = shuffled.Courts[j], shuffled.Courts[i] })
			r.Shuffle(len(shuffled.JudgeIDs), func(i, j int) {
				shuffled.JudgeIDs[i], shuffled.JudgeIDs[j] = shuffled.JudgeIDs[j], shuffled.JudgeIDs[i]
			})
			if got := runMatcher(shuffled); !reflect.DeepEqual(got, want) {
				t.Fatalf("round %d: a shuffled input gave a different schedule", round)
			}
		}
	}
}

func TestMatcherInvariantsOnRandomEvents(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	placed, total := 0, 0
	for range 300 {
		in := randomInput(r)
		s := runChecked(t, in)
		total += len(in.Entries)
		for _, m := range s.Matches {
			placed += len(m.Sides[0].Players) * 2
		}
	}
	// Not a rule, a sanity check that the matcher isn't benching wholesale.
	if placed*10 < total*6 {
		t.Errorf("placed only %d of %d entries", placed, total)
	}
}

func TestMatcherDoesNotChangeItsInput(t *testing.T) {
	in := randomInput(rand.New(rand.NewPCG(5, 6)))
	before := slices.Clone(in.Entries)
	for i := range before {
		before[i].Formats = slices.Clone(before[i].Formats)
	}
	runMatcher(in)
	if !reflect.DeepEqual(before, in.Entries) {
		t.Error("runMatcher modified its input")
	}
}

func TestMatcherEmpty(t *testing.T) {
	s := runChecked(t, baseInput())
	if s.Matches == nil || s.Standby == nil || s.Warnings == nil || len(s.Matches)+len(s.Standby)+len(s.Warnings) != 0 {
		t.Errorf("schedule = %+v; want empty, non-nil slices", s)
	}
}
