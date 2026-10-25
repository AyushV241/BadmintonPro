import type { GameScore, Side } from "@/lib/api";

// The same rules as backend/scoring.go: rally scoring to 21, win by 2,
// capped at 30, best of 3. The server is the source of truth; this only lets
// the judge's console show taps instantly, before the server confirms them.
const POINTS_TO_WIN = 21;
const POINTS_CAP = 30;
const GAMES_TO_WIN = 2;

export function gameWon(a: number, b: number) {
  if (a === POINTS_CAP || b === POINTS_CAP) return true;
  return (a >= POINTS_TO_WIN || b >= POINTS_TO_WIN) && Math.abs(a - b) >= 2;
}

export type Replayed = {
  games: GameScore[];
  gamesA: number;
  gamesB: number;
  server: Side;
  winner?: Side;
};

/** Replays the rallies that count, oldest first, from the toss. */
export function replay(firstServer: Side, rallies: Side[]): Replayed {
  const r: Replayed = { games: [{ a: 0, b: 0 }], gamesA: 0, gamesB: 0, server: firstServer };
  for (const side of rallies) {
    if (r.winner) break;
    const cur = r.games[r.games.length - 1];
    if (side === "A") cur.a++;
    else cur.b++;
    r.server = side; // the rally winner serves next
    if (gameWon(cur.a, cur.b)) {
      cur.winner = side;
      if (side === "A") r.gamesA++;
      else r.gamesB++;
      if (r.gamesA === GAMES_TO_WIN || r.gamesB === GAMES_TO_WIN) r.winner = side;
      else r.games.push({ a: 0, b: 0 }); // the game's winner serves first in the next
    }
  }
  return r;
}

/** "Game point", "Match point", or "" for the side about to serve. */
export function pointLabel(r: Replayed): string {
  if (r.winner) return "";
  const cur = r.games[r.games.length - 1];
  const nextA = gameWon(cur.a + 1, cur.b);
  const nextB = gameWon(cur.a, cur.b + 1);
  if (!nextA && !nextB) return "";
  const matchA = nextA && r.gamesA + 1 === GAMES_TO_WIN;
  const matchB = nextB && r.gamesB + 1 === GAMES_TO_WIN;
  return matchA || matchB ? "Match point" : "Game point";
}
