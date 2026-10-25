"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { BottomNav } from "@/components/app/BottomNav";
import { formatName } from "@/components/events/EventBits";
import { ChevronLeftIcon } from "@/components/icons";
import { GameChips, LiveDot, SideAvatars, headlinesGames, sideName, sideOf } from "@/components/live/LiveBits";
import { ApiError, matchOver, meSession, ratingChangeFor, watchMatch, type LiveMatch, type Side } from "@/lib/api";
import { clockTime } from "@/lib/eventTime";

/** "40s", "3m", "1h" since a point. */
function ago(iso: string, now: number) {
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 5) return "now";
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${Math.floor(s / 3600)}h`;
}

function ScoreCard({ match, meId }: { match: LiveMatch; meId: string }) {
  const tz = match.venue.timezone;
  const mySide = match.sides.find((s) => s.players.some((p) => p.userId === meId))?.side;
  const a = sideOf(match.sides, "A");
  const b = sideOf(match.sides, "B");
  const games = match.score.games;
  const cur = games[games.length - 1] ?? { a: 0, b: 0 };
  const live = match.status === "live";
  const over = matchOver(match.status);
  // Over with no game won (retired or voided early): the points, not 0:0 in games.
  const showGames = over && headlinesGames(match.score);
  const decided = over || Boolean(match.score.winner);
  const label = live
    ? `LIVE · GAME ${Math.max(1, games.length)}`
    : over
      ? match.status === "completed"
        ? "FINAL"
        : match.status.toUpperCase()
      : `STARTS ${clockTime(match.startsAt, tz)}`;

  const column = (s: Side) => {
    const side = s === "A" ? a : b;
    return (
      <div className="flex min-w-0 flex-col items-center gap-1.5 text-center">
        <SideAvatars side={side} highlight={mySide === s} size={48} />
        <span className="max-w-full truncate text-sm font-semibold">{sideName(side, meId)}</span>
        {live && match.score.server === s ? (
          <span className="rounded-full bg-lime px-2 py-0.5 text-[10px] font-semibold tracking-[0.08em] text-ink">SERVING</span>
        ) : (
          <span className="text-[11px] text-[#a3a596]">Rating {side?.rating ?? "–"}</span>
        )}
      </div>
    );
  };

  return (
    <section
      aria-label="Live match"
      className="flex flex-col gap-3.5 rounded-3xl bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0]"
    >
      <div className="flex items-center justify-between gap-2 text-[11px] font-semibold tracking-[0.12em]">
        <span className="flex shrink-0 items-center gap-1.5 whitespace-nowrap">
          {live && <LiveDot />}
          {label}
        </span>
        <span className="min-w-0 truncate text-[#a3a596] uppercase">
          {match.event?.title ?? match.venue.name} · {match.court.label}
        </span>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-1.5">
        {column("A")}
        <div
          aria-live="polite"
          aria-label={showGames ? `Games ${match.score.gamesA} to ${match.score.gamesB}` : `Score ${cur.a} to ${cur.b}`}
          className="flex items-center gap-2 text-[60px] leading-none font-bold tracking-[-0.05em] tabular-nums"
        >
          {match.status === "scheduled" || match.status === "ready" ? (
            <span className="text-[38px] text-[#a3a596]">VS</span>
          ) : over && match.firstServer === "" ? (
            // Never started: a walkover, or struck off before the first rally.
            <span className="text-[38px] text-[#a3a596]">{match.status === "walkover" ? "W/O" : "–"}</span>
          ) : over ? (
            <span className="flex flex-col items-center gap-1">
              <span className="flex items-center gap-2">
                <span className={mySide === "A" ? "text-lime" : ""}>{showGames ? match.score.gamesA : cur.a}</span>
                <span className="text-[38px] text-[#a3a596]">:</span>
                <span className={mySide === "B" ? "text-lime" : ""}>{showGames ? match.score.gamesB : cur.b}</span>
              </span>
              <span className="text-[11px] font-semibold tracking-[0.14em] text-[#a3a596]">
                {showGames ? "GAMES" : "POINTS"}
              </span>
            </span>
          ) : (
            <>
              <span className={mySide === "A" ? "text-lime" : ""}>{cur.a}</span>
              <span className="text-[38px] text-[#a3a596]">:</span>
              <span className={mySide === "B" ? "text-lime" : ""}>{cur.b}</span>
            </>
          )}
        </div>
        {column("B")}
      </div>
      {games.length > 0 && (
        <div className="flex flex-col items-center gap-2">
          <span className="text-[11px] font-semibold tracking-[0.14em] text-[#a3a596] uppercase">
            Best of 3 · {formatName(match.format)}
          </span>
          <GameChips games={games} decided={decided} />
        </div>
      )}
      {over && <Outcome match={match} meId={meId} mySide={mySide} />}
      <div className="flex items-center gap-2 border-t border-[#262920] pt-3 text-xs text-[#a3a596]">
        {match.judge ? `Judge ${match.judge.name}` : "No judge yet"}
        {live && " · updates live"}
        {!live && !over && " · scoring starts when the judge starts the match"}
      </div>
    </section>
  );
}

/** How the match ended, and what it did to the viewer's rating. */
function Outcome({ match, meId, mySide }: { match: LiveMatch; meId: string; mySide?: Side }) {
  const winnerSide = match.winnerSide;
  const name = (s: Side) => sideName(sideOf(match.sides, s), meId);
  let headline: string | null = null;
  let detail: string | null = null;
  if (match.status === "void") {
    headline = "Void: this match doesn't count";
    detail = match.void?.reason ?? null;
  } else if (winnerSide) {
    const loserSide: Side = winnerSide === "A" ? "B" : "A";
    const won = mySide === winnerSide;
    switch (match.status) {
      case "retired":
        headline = mySide ? (won ? "You won" : "You retired") : `${name(winnerSide)} won`;
        if (!mySide || won) detail = `${name(loserSide)} retired`;
        break;
      case "walkover":
        headline = mySide ? (won ? "You won by walkover" : `Walkover to ${name(winnerSide)}`) : `${name(winnerSide)} won by walkover`;
        break;
      default:
        headline = mySide ? (won ? "You won" : "You lost") : `${name(winnerSide)} won`;
    }
  }
  if (!headline) return null;

  const mine = mySide ? ratingChangeFor(match, meId) : null;
  let rating: string | null = null;
  if (mine && match.status === "void") rating = `Your rating change was reversed: now ${mine.after}`;
  else if (mine) rating = `Rating ${mine.after} (${mine.change >= 0 ? "+" : "−"}${Math.abs(mine.change)})`;
  else if (mySide && match.status !== "void" && match.status !== "cancelled") rating = "No rating change";

  return (
    <div className="flex flex-col items-center gap-1 text-center">
      <p className="text-lg font-semibold">{headline}</p>
      {detail && <p className="text-sm text-[#a3a596]">{detail}</p>}
      {rating && (
        <p className="mt-1 rounded-full bg-[#262920] px-3 py-1.5 text-xs font-semibold tracking-[0.04em] text-lime tabular-nums">
          {rating}
        </p>
      )}
    </div>
  );
}

function PointByPoint({ match, meId, now }: { match: LiveMatch; meId: string; now: number }) {
  const rallies = match.score.rallies.slice(0, 12);
  if (rallies.length === 0) return null;
  return (
    <section aria-labelledby="pbp" className="flex flex-col gap-1.5">
      <h2 id="pbp" className="text-[17px] font-semibold tracking-[-0.02em]">
        Point by point
      </h2>
      <ol className="rounded-[18px] border border-line bg-surface">
        {rallies.map((r, i) => (
          <li
            key={r.seq}
            className={`flex items-center gap-2.5 px-3.5 py-3 text-sm ${i < rallies.length - 1 ? "border-b border-line" : ""}`}
          >
            <span
              aria-hidden="true"
              className={`size-2 rounded-full ${r.side === "A" ? "bg-[#c9b45a]" : "bg-[#9cc2f5]"}`}
            />
            <span className="flex-1">Point · {sideName(sideOf(match.sides, r.side), meId)}</span>
            <span className="font-semibold tabular-nums">
              {r.a}–{r.b}
            </span>
            <span className="w-[38px] text-right text-xs text-muted">{ago(r.at, now)}</span>
          </li>
        ))}
      </ol>
    </section>
  );
}

/** A player's live view of a match: the score updates as the judge taps. */
export default function LiveMatchPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [meId, setMeId] = useState("");
  const [match, setMatch] = useState<LiveMatch | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reconnecting, setReconnecting] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    meSession()
      .then((s) => setMeId(s.user.id))
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) router.replace("/login");
      });
  }, [router]);

  useEffect(
    () =>
      watchMatch(
        id,
        (m) => {
          setMatch(m);
          setReconnecting(false);
          setError(null);
        },
        () => setReconnecting(true),
      ),
    [id],
  );

  // Fallback when the stream can't open at all (not signed in, unknown match).
  useEffect(() => {
    if (match || !reconnecting) return;
    const t = setTimeout(() => setError("Couldn't load this match. Check your connection."), 6000);
    return () => clearTimeout(t);
  }, [match, reconnecting]);

  // Keeps "40s ago" fresh.
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 10_000);
    return () => clearInterval(t);
  }, []);

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col gap-4 px-5 pt-4 pb-36">
        <header className="flex items-center gap-1">
          <Link href="/matches" aria-label="Back to your matches" className="-ml-2 flex size-11 items-center justify-center">
            <ChevronLeftIcon className="size-[22px]" />
          </Link>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">Your games</span>
            <h1 className="text-[28px] leading-tight font-medium tracking-[-0.04em]">Live match</h1>
          </div>
        </header>

        {error && (
          <p role="alert" className="rounded-[20px] border border-line bg-surface p-4 text-sm text-muted">
            {error}
          </p>
        )}
        {!match && !error && <div aria-busy="true" className="h-[300px] animate-pulse rounded-3xl bg-line" />}
        {match && (
          <>
            {reconnecting && !matchOver(match.status) && (
              <p role="status" className="text-center text-xs text-muted">
                Reconnecting…
              </p>
            )}
            <ScoreCard match={match} meId={meId} />
            <PointByPoint match={match} meId={meId} now={now} />
          </>
        )}
      </div>
      <BottomNav active="matches" />
    </main>
  );
}
