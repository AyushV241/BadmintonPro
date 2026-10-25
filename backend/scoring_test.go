package main

import "testing"

// rallies turns a string like "AAB" into point events, numbered from 1.
func rallies(sides string) []PointEvent {
	evs := make([]PointEvent, len(sides))
	for i, c := range sides {
		evs[i] = PointEvent{Seq: i + 1, Kind: PointRally, Side: string(c)}
	}
	return evs
}

func repeat(side string, n int) string {
	s := ""
	for range n {
		s += side
	}
	return s
}

func TestComputeScoreBeforeStart(t *testing.T) {
	s := computeScore("", nil)
	if len(s.Games) != 0 || s.Server != "" || s.Winner != "" {
		t.Errorf("score = %+v, want empty", s)
	}
}

func TestComputeScoreServeFollowsTheRally(t *testing.T) {
	s := computeScore("B", rallies("AAB"))
	if g := s.Games[0]; g.A != 2 || g.B != 1 || g.Winner != "" {
		t.Errorf("game = %+v, want 2–1 in play", g)
	}
	if s.Server != "B" {
		t.Errorf("server = %q, want B (won the last rally)", s.Server)
	}
	if len(s.Rallies) != 3 || s.Rallies[0].Seq != 3 || s.Rallies[0].A != 2 || s.Rallies[0].B != 1 {
		t.Errorf("rallies = %+v, want newest first with the running score", s.Rallies)
	}
}

func TestComputeScoreGameRules(t *testing.T) {
	cases := []struct {
		name   string
		points string
		done   bool
	}{
		{"21–19 wins", repeat("A", 19) + repeat("B", 19) + "AA", true},
		{"21–20 doesn't", repeat("A", 20) + repeat("B", 20) + "A", false},
		{"22–20 wins after deuce", repeat("A", 20) + repeat("B", 20) + "AA", true},
		{"29–29 then 30 wins", repeat("AB", 29) + "A", true},
		{"21–0 wins", repeat("A", 21), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := computeScore("A", rallies(c.points))
			if got := s.Games[0].Winner != ""; got != c.done {
				t.Errorf("game over = %v, want %v (score %+v)", got, c.done, s.Games[0])
			}
		})
	}
}

func TestComputeScoreBestOfThree(t *testing.T) {
	// A takes game 1, B game 2, A game 3.
	log := repeat("A", 21) + repeat("B", 21) + repeat("A", 21)
	s := computeScore("A", rallies(log))
	if len(s.Games) != 3 || s.GamesA != 2 || s.GamesB != 1 || s.Winner != "A" {
		t.Fatalf("score = games %+v, %d–%d, winner %q", s.Games, s.GamesA, s.GamesB, s.Winner)
	}
	// The winner of game 2 served first in game 3.
	s2 := computeScore("A", rallies(repeat("A", 21)+repeat("B", 21)))
	if s2.Server != "B" || len(s2.Games) != 3 || s2.Games[2] != (GameScore{}) {
		t.Errorf("after game 2: server %q, games %+v; want B to serve a fresh game 3", s2.Server, s2.Games)
	}
	// Straight games end the match after two.
	s3 := computeScore("B", rallies(repeat("B", 42)))
	if len(s3.Games) != 2 || s3.Winner != "B" {
		t.Errorf("straight games: %+v, winner %q", s3.Games, s3.Winner)
	}
}

func TestComputeScoreUndo(t *testing.T) {
	evs := append(rallies("AAB"), PointEvent{Seq: 4, Kind: PointUndo})
	s := computeScore("A", evs)
	if g := s.Games[0]; g.A != 2 || g.B != 0 {
		t.Errorf("after undo: %+v, want 2–0", g)
	}
	if s.Server != "A" {
		t.Errorf("server = %q, want A again (won the rally before the undone one)", s.Server)
	}
	// Undoing past the start serves from the toss.
	evs = []PointEvent{{Seq: 1, Kind: PointRally, Side: "A"}, {Seq: 2, Kind: PointUndo}}
	if s := computeScore("B", evs); s.Server != "B" || s.Games[0].A != 0 || s.points != 0 {
		t.Errorf("undo to start: %+v", s)
	}
	// Undo can reopen a decided game.
	evs = append(rallies(repeat("A", 21)), PointEvent{Seq: 22, Kind: PointUndo})
	if s := computeScore("A", evs); len(s.Games) != 1 || s.Games[0].A != 20 || s.Games[0].Winner != "" {
		t.Errorf("undo game point: %+v", s.Games)
	}
}
