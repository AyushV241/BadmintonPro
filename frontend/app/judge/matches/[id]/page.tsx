"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formatName } from "@/components/events/EventBits";
import { ChevronLeftIcon } from "@/components/icons";
import { GameChips, LiveDot, SideAvatars, sideName, sideOf } from "@/components/live/LiveBits";
import { BottomSheet, Button, useBottomSheet } from "@/components/ui";
import {
  ApiError,
  judgeFinishMatch,
  judgeRetireMatch,
  judgeSendPoint,
  judgeStartMatch,
  judgeWalkover,
  liveMatch,
  matchOver,
  type LiveMatch,
  type MatchStatus,
  type Side,
} from "@/lib/api";
import { clockTime } from "@/lib/eventTime";
import { pointLabel, replay } from "@/lib/scoring";

/** A tap the server hasn't confirmed yet. */
type Pending = { kind: "point"; side: Side; clientId: string } | { kind: "undo"; clientId: string };

const queueKey = (id: string) => `bp.judge.queue.${id}`;

// Unsent taps survive a reload or a dropped connection. Storage can be
// unavailable (private mode), in which case the queue lives only in memory.
function loadQueue(id: string): Pending[] {
  try {
    const raw = window.localStorage.getItem(queueKey(id));
    return raw ? (JSON.parse(raw) as Pending[]) : [];
  } catch {
    return [];
  }
}

function saveQueue(id: string, queue: Pending[]) {
  try {
    if (queue.length) window.localStorage.setItem(queueKey(id), JSON.stringify(queue));
    else window.localStorage.removeItem(queueKey(id));
  } catch {
    /* in memory only */
  }
}

const newClientId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

const titles: Partial<Record<MatchStatus, string>> = {
  completed: "Match finished",
  retired: "Match ended early",
  walkover: "Walkover",
  void: "Match void",
  cancelled: "Match cancelled",
};

const other = (s: Side): Side => (s === "A" ? "B" : "A");

/** One button per side, for "who turned up" and "who can't go on". */
function SidePicker({
  legend,
  match,
  picked,
  onPick,
}: {
  legend: string;
  match: LiveMatch;
  picked: Side | null;
  onPick: (s: Side) => void;
}) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-2 text-[15px] font-semibold">{legend}</legend>
      <div className="grid grid-cols-2 gap-2">
        {(["A", "B"] as const).map((s) => (
          <button
            key={s}
            type="button"
            aria-pressed={picked === s}
            onClick={() => onPick(s)}
            className={`min-h-[54px] rounded-2xl px-2 text-[15px] font-semibold ${
              picked === s ? "bg-ink text-white" : "border border-line bg-surface"
            }`}
          >
            {sideName(sideOf(match.sides, s))}
          </button>
        ))}
      </div>
    </fieldset>
  );
}

/** For a side that never turned up: the match goes to the other, unrated. */
function WalkoverSheet({ match, onDone }: { match: LiveMatch; onDone: (m: LiveMatch) => void }) {
  const sheet = useBottomSheet("walkover");
  const [winner, setWinner] = useState<Side | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const absent = winner && sideName(sideOf(match.sides, other(winner)));

  async function record() {
    if (!winner) return;
    setBusy(true);
    setError(null);
    try {
      const m = await judgeWalkover(match.id, winner);
      sheet.close();
      onDone(m);
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <BottomSheet
      hash="walkover"
      title="Someone didn't turn up"
      description="The match goes to the side that's here. Nobody's rating changes."
      footer={
        <Button variant="danger" size="lg" fullWidth loading={busy} disabled={!winner} onClick={() => void record()}>
          Record walkover
        </Button>
      }
    >
      <div className="flex flex-col gap-3 pb-2">
        <SidePicker legend="Who turned up?" match={match} picked={winner} onPick={setWinner} />
        {absent && (
          <p className="text-sm text-muted">
            {absent} will be marked as a no-show, so their entry fee isn&rsquo;t refunded.
          </p>
        )}
        {error && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        )}
      </div>
    </BottomSheet>
  );
}

/**
 * For a side that can't go on (an injury): the other side wins, and ratings
 * follow the score so far.
 */
function RetireSheet({
  match,
  waiting,
  onDone,
}: {
  match: LiveMatch;
  /** Taps not yet sent: the score must be complete first. */
  waiting: number;
  onDone: (m: LiveMatch) => void;
}) {
  const sheet = useBottomSheet("retire");
  const [retired, setRetired] = useState<Side | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const games = match.score.games;
  const played = games.reduce((n, g) => n + g.a + g.b, 0);
  const sofar = games
    .filter((g) => g.a + g.b > 0)
    .map((g) => `${g.a}–${g.b}`)
    .join(", ");

  async function record() {
    if (!retired) return;
    setBusy(true);
    setError(null);
    try {
      const m = await judgeRetireMatch(match.id, retired);
      sheet.close();
      onDone(m);
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <BottomSheet
      hash="retire"
      title="End the match early"
      description="For a player who's injured or can't go on. The other side wins."
      footer={
        <Button
          variant="danger"
          size="lg"
          fullWidth
          loading={busy}
          disabled={!retired || waiting > 0}
          onClick={() => void record()}
        >
          {retired ? `${sideName(sideOf(match.sides, retired))} retired` : "End match"}
        </Button>
      }
    >
      <div className="flex flex-col gap-3 pb-2">
        <SidePicker legend="Who can't go on?" match={match} picked={retired} onPick={setRetired} />
        {retired && (
          <p className="text-sm text-muted">
            The match goes to {sideName(sideOf(match.sides, other(retired)))}.{" "}
            {played > 0
              ? `Ratings follow the score so far (${sofar}): whoever is ahead on points gains, even if they retire.`
              : "No points were played, so nobody's rating changes."}
          </p>
        )}
        {waiting > 0 && (
          <p role="status" className="rounded-xl bg-[#fdf0d9] px-3 py-2 text-sm text-[#8a4b05]">
            {waiting} {waiting === 1 ? "point is" : "points are"} still sending. You can end the match once
            they&rsquo;re in.
          </p>
        )}
        {error && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        )}
      </div>
    </BottomSheet>
  );
}

function Header({ match }: { match: LiveMatch }) {
  const live = match.status === "live";
  const gameNo = Math.max(1, match.score.games.length);
  return (
    <header className="flex shrink-0 items-center gap-1.5 px-3 pt-3.5 pb-2">
      <Link href="/judge" aria-label="Back to your matches" className="flex size-11 items-center justify-center">
        <ChevronLeftIcon className="size-[22px]" />
      </Link>
      <div className="flex flex-1 flex-col gap-0.5">
        <span className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">
          Judge · {match.court.label} · {formatName(match.format)}
        </span>
        <h1 className="text-2xl font-semibold tracking-[-0.04em]">
          {live ? `Game ${gameNo} of 3` : (titles[match.status] ?? "Ready to start")}
        </h1>
      </div>
      {live && (
        <span className="mr-2 flex items-center gap-1.5 rounded-full bg-[#fde8e8] px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-[#c8321f]">
          <LiveDot />
          LIVE
        </span>
      )}
    </header>
  );
}

function StartPanel({ match, onStarted }: { match: LiveMatch; onStarted: (m: LiveMatch) => void }) {
  const [server, setServer] = useState<Side>("A");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const tz = match.venue.timezone;
  const [tooEarly] = useState(() => Date.now() < Date.parse(match.canStartFrom));
  const walkover = useBottomSheet("walkover");
  const a = sideOf(match.sides, "A");
  const b = sideOf(match.sides, "B");

  async function start() {
    setBusy(true);
    setError(null);
    try {
      onStarted(await judgeStartMatch(match.id, server));
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <main className="flex flex-1 flex-col gap-4 px-4 pt-1">
      <section className="flex flex-col gap-4 rounded-[26px] bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0]">
        <div className="flex items-center justify-between text-[11px] font-semibold tracking-[0.12em]">
          <span className="rounded-full bg-[#262920] px-2.5 py-1.5 text-lime">{match.event?.title ?? match.venue.name}</span>
          <span className="text-[#a3a596]">{clockTime(match.startsAt, tz)}</span>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
          {[a, b].map((s, i) => (
            <div key={i} className={`flex flex-col items-center gap-1.5 text-center ${i ? "col-start-3" : ""}`}>
              <SideAvatars side={s} size={52} />
              <span className="max-w-full truncate text-[15px] font-semibold">{sideName(s)}</span>
              <span className="text-xs text-[#a3a596]">Rating {s?.rating ?? "–"}</span>
            </div>
          ))}
          <span className="col-start-2 row-start-1 text-xl font-semibold text-[#a3a596]">VS</span>
        </div>
      </section>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-[15px] font-semibold">Who serves first? (the toss)</legend>
        <div className="grid grid-cols-2 gap-2">
          {(["A", "B"] as const).map((s) => (
            <button
              key={s}
              type="button"
              aria-pressed={server === s}
              onClick={() => setServer(s)}
              className={`min-h-[54px] rounded-2xl text-[15px] font-semibold ${
                server === s ? "bg-ink text-white" : "border border-line bg-surface"
              }`}
            >
              {sideName(s === "A" ? a : b)}
            </button>
          ))}
        </div>
      </fieldset>

      <div className="mt-auto flex flex-col gap-2 pb-7">
        {error && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        )}
        {tooEarly && (
          <p className="text-center text-sm text-muted">You can start from {clockTime(match.canStartFrom, tz)}.</p>
        )}
        <Button size="lg" variant="accent" fullWidth loading={busy} disabled={tooEarly} onClick={start}>
          Start match
        </Button>
        <Button variant="ghost" fullWidth disabled={tooEarly || busy} onClick={walkover.open}>
          Someone didn&rsquo;t turn up
        </Button>
      </div>
      <WalkoverSheet match={match} onDone={onStarted} />
    </main>
  );
}

function Finished({ match }: { match: LiveMatch }) {
  const voided = match.status === "void";
  const winner = match.winnerSide && !voided ? sideOf(match.sides, match.winnerSide) : undefined;
  const loser = match.winnerSide ? sideName(sideOf(match.sides, other(match.winnerSide))) : "";
  const badge =
    match.status === "completed"
      ? "RESULT RECORDED"
      : match.status === "walkover"
        ? "WALKOVER"
        : match.status.toUpperCase();
  const note = voided
    ? match.void?.reason
    : match.status === "retired"
      ? `${loser} retired`
      : match.status === "walkover"
        ? `${loser} didn't turn up`
        : null;
  const played = match.score.games.some((g) => g.a + g.b > 0);
  return (
    <main className="flex flex-1 flex-col gap-4 px-4 pt-1">
      <section className="flex flex-col items-center gap-3 rounded-[26px] bg-ink p-6 text-center text-[#f5f5f0]">
        <span className="rounded-full bg-[#262920] px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-lime">
          {badge}
        </span>
        {!voided && <SideAvatars side={winner} highlight size={58} />}
        <p className="text-2xl font-semibold tracking-[-0.03em]">
          {winner ? `${sideName(winner)} won` : voided ? "This match doesn't count" : "No result"}
        </p>
        {note && <p className="text-sm text-[#a3a596]">{note}</p>}
        {played && <GameChips games={match.score.games} decided />}
      </section>
      <div className="mt-auto pb-7">
        <Button href="/judge" variant="secondary" size="lg" fullWidth>
          Back to your matches
        </Button>
      </div>
    </main>
  );
}

/** The scoring console: big +Point buttons, instant taps, an offline queue. */
function Console({ match, setMatch }: { match: LiveMatch; setMatch: (m: LiveMatch) => void }) {
  const id = match.id;
  const [queue, setQueue] = useState<Pending[]>(() => loadQueue(id));
  const [offline, setOffline] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [finishing, setFinishing] = useState(false);
  const [retry, setRetry] = useState(0);
  const retire = useBottomSheet("retire");
  const sending = useRef(false);

  useEffect(() => saveQueue(id, queue), [id, queue]);

  // Keep the screen awake while scoring, where the browser allows it.
  useEffect(() => {
    let lock: { release: () => Promise<void> } | null = null;
    const nav = navigator as Navigator & { wakeLock?: { request: (t: "screen") => Promise<{ release: () => Promise<void> }> } };
    nav.wakeLock?.request("screen").then((l) => (lock = l)).catch(() => {});
    return () => void lock?.release().catch(() => {});
  }, []);

  // Sends the queue one tap at a time, in order. The server numbers the log,
  // so each send carries the nextSeq it last reported.
  useEffect(() => {
    const head = queue[0];
    if (!head || sending.current || match.status !== "live") return;
    sending.current = true;
    judgeSendPoint(id, { ...head, seq: match.nextSeq })
      .then((m) => {
        setMatch(m);
        setQueue((q) => q.slice(1));
        setOffline(false);
      })
      .catch(async (err: unknown) => {
        if (err instanceof ApiError && err.status === 409 && err.message.startsWith("out of order")) {
          // Out of turn: reload for the real nextSeq and send again. A tap the
          // server already has is recognised by its clientId.
          const m = await liveMatch(id).catch(() => null);
          if (m) setMatch(m);
        } else if (!(err instanceof ApiError) || err.status === 0 || err.status >= 500) {
          // Offline or the server is down: keep the tap and retry shortly.
          setOffline(true);
          await new Promise((r) => setTimeout(r, 2000));
        } else {
          // Refused (the match was decided or finished): drop it, and say why.
          setQueue((q) => q.slice(1));
          setError(err.message);
          const m = await liveMatch(id).catch(() => null);
          if (m) setMatch(m);
        }
      })
      .finally(() => {
        sending.current = false;
        setRetry((n) => n + 1);
      });
  }, [id, queue, match, retry, setMatch]);

  // The score as the judge sees it: the server's, plus unconfirmed taps.
  const view = useMemo(() => {
    const counted: Side[] = [...match.score.rallies].reverse().map((r) => r.side);
    for (const p of queue) {
      if (p.kind === "point") counted.push(p.side);
      else counted.pop();
    }
    return { ...replay((match.firstServer || "A") as Side, counted), points: counted.length };
  }, [match, queue]);

  const tap = useCallback((side: Side) => {
    setError(null);
    setQueue((q) => [...q, { kind: "point", side, clientId: newClientId() }]);
  }, []);

  function undo() {
    setError(null);
    const last = queue[queue.length - 1];
    // An unsent tap is just taken off the queue; a recorded one needs an undo.
    if (last && last.kind === "point" && queue.length > 1) setQueue((q) => q.slice(0, -1));
    else if (view.points > 0) setQueue((q) => [...q, { kind: "undo", clientId: newClientId() }]);
  }

  async function finish() {
    setFinishing(true);
    try {
      setMatch(await judgeFinishMatch(id));
    } catch (err) {
      setError(message(err));
      setFinishing(false);
    }
  }

  if (matchOver(match.status)) return <Finished match={match} />;

  const a = sideOf(match.sides, "A");
  const b = sideOf(match.sides, "B");
  const cur = view.games[view.games.length - 1];
  const decided = Boolean(view.winner);
  const status = decided
    ? `Match to ${sideName(view.winner === "A" ? a : b)}`
    : pointLabel(view) || "Rally on";

  return (
    <>
      <main className="flex min-h-0 flex-1 flex-col gap-3 px-4 pt-1">
        <section
          aria-label="Score"
          className="flex flex-col gap-3.5 rounded-[26px] bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] px-4 py-[18px] text-[#f5f5f0]"
        >
          <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-end gap-1">
            {(["A", "B"] as const).map((s, i) => {
              const side = s === "A" ? a : b;
              const score = s === "A" ? cur.a : cur.b;
              return (
                <div key={s} className={`flex flex-col items-center gap-1.5 ${i ? "col-start-3" : ""}`}>
                  <SideAvatars side={side} size={44} />
                  <span className="max-w-full truncate text-sm font-semibold">{sideName(side)}</span>
                  <span className="text-xs text-[#a3a596]">Games {s === "A" ? view.gamesA : view.gamesB}</span>
                  <div className="flex h-[22px] items-center">
                    {!decided && view.server === s && (
                      <span className="rounded-full bg-lime px-2 py-0.5 text-[10px] font-semibold tracking-[0.08em] text-ink">
                        SERVING
                      </span>
                    )}
                  </div>
                  <span aria-live="polite" className="text-[92px] leading-[0.9] font-bold tracking-[-0.05em] tabular-nums">
                    {score}
                  </span>
                </div>
              );
            })}
            <span className="col-start-2 row-start-1 pb-3.5 text-[40px] font-semibold text-[#a3a596]">:</span>
          </div>
          <GameChips games={view.games} decided={decided} />
          <span className="self-center rounded-full bg-[#262920] px-3 py-1.5 text-xs font-semibold tracking-[0.08em] text-lime uppercase">
            {status}
          </span>
        </section>

        {(offline || queue.length > 1) && (
          <p role="status" className="rounded-xl bg-[#fdf0d9] px-3 py-2 text-center text-sm text-[#8a4b05]">
            {offline ? "No connection. " : ""}
            {queue.length} {queue.length === 1 ? "point" : "points"} waiting to send. Keep scoring.
          </p>
        )}
        {error && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        )}

        <div className="grid min-h-[160px] flex-1 grid-cols-2 gap-2.5">
          <button
            type="button"
            onClick={() => tap("A")}
            disabled={decided}
            aria-label={`Point to ${sideName(a)}`}
            className="flex flex-col items-center justify-center gap-1 rounded-3xl bg-lime text-ink disabled:opacity-45"
          >
            <span className="text-[30px] font-semibold tracking-[-0.035em]">+ Point</span>
            <span className="text-[15px] font-semibold">{sideName(a)}</span>
          </button>
          <button
            type="button"
            onClick={() => tap("B")}
            disabled={decided}
            aria-label={`Point to ${sideName(b)}`}
            className="flex flex-col items-center justify-center gap-1 rounded-3xl border-2 border-ink text-ink disabled:opacity-45"
          >
            <span className="text-[30px] font-semibold tracking-[-0.035em]">+ Point</span>
            <span className="text-[15px] font-semibold">{sideName(b)}</span>
          </button>
        </div>
        <Button variant="ghost" size="sm" fullWidth disabled={decided} onClick={retire.open}>
          Player injured? End the match early
        </Button>
      </main>
      <RetireSheet match={match} waiting={queue.length} onDone={setMatch} />
      <footer className="grid shrink-0 grid-cols-2 gap-2.5 px-4 pt-3 pb-7">
        <Button variant="secondary" size="lg" fullWidth onClick={undo} disabled={view.points === 0}>
          Undo
        </Button>
        <Button
          variant={decided ? "accent" : "secondary"}
          size="lg"
          fullWidth
          loading={finishing}
          disabled={!decided || queue.length > 0}
          onClick={finish}
        >
          Finish match
        </Button>
      </footer>
    </>
  );
}

export default function JudgeMatchPage() {
  const { id } = useParams<{ id: string }>();
  const [match, setMatch] = useState<LiveMatch | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    liveMatch(id)
      .then(setMatch)
      .catch((err) => setError(message(err)));
  }, [id]);

  if (error) {
    return (
      <main className="flex min-h-dvh flex-col items-center justify-center gap-3 bg-background px-6 text-center">
        <p className="text-sm text-muted">{error}</p>
        <Button href="/judge" variant="secondary">
          Back to your matches
        </Button>
      </main>
    );
  }
  if (!match) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  return (
    <div className="flex min-h-dvh justify-center bg-background text-foreground">
      <div className="flex min-h-dvh w-full max-w-[480px] flex-col">
        <Header match={match} />
        {match.status === "scheduled" || match.status === "ready" ? (
          <StartPanel match={match} onStarted={setMatch} />
        ) : match.status === "live" ? (
          <Console match={match} setMatch={setMatch} />
        ) : (
          <Finished match={match} />
        )}
      </div>
    </div>
  );
}
