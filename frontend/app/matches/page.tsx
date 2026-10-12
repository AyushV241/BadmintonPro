"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { BottomNav } from "@/components/app/BottomNav";
import { DateTile, formatName } from "@/components/events/EventBits";
import { directionsUrl } from "@/components/events/VenueMap";
import { CheckIcon, PinIcon } from "@/components/icons";
import { ApiError, me, myEntries, myMatches, type Entry, type MatchSide, type MyMatch, type User } from "@/lib/api";
import { clockTime, dateTile, rupees } from "@/lib/eventTime";
import { initials } from "@/lib/initials";

/** Players check in this long before their match (design doc, section 4.4). */
const CHECK_IN_MINUTES = 15;

const palette = [avatarColors.blue, avatarColors.pink, avatarColors.peach, avatarColors.sand];

/** A stable avatar colour per player. */
function colorFor(userId: string) {
  let h = 0;
  for (const c of userId) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return palette[h % palette.length];
}

function sideNames(side: MatchSide, meId: string) {
  return side.players.map((p) => (p.userId === meId ? "You" : p.name.split(" ")[0])).join(" & ");
}

function SideBlock({ side, meId }: { side: MatchSide; meId: string }) {
  return (
    <div className="flex min-w-0 flex-col items-center gap-1.5 text-center">
      <span className="flex">
        {side.players.map((p, i) => (
          <span key={p.userId} className={i ? "-ml-3" : ""}>
            <Avatar
              text={initials(p.name, p.username)}
              color={p.userId === meId ? avatarColors.lime : colorFor(p.userId)}
              size={side.players.length > 1 ? 46 : 58}
            />
          </span>
        ))}
      </span>
      <span className="max-w-full truncate text-[15px] font-semibold">{sideNames(side, meId)}</span>
      <span className="text-xs text-[#a3a596]">Rating {side.rating}</span>
    </div>
  );
}

function NextMatch({ match, meId }: { match: MyMatch; meId: string }) {
  const tz = match.venue.timezone;
  const mine = match.sides.find((s) => s.side === match.mySide)!;
  const theirs = match.sides.find((s) => s.side !== match.mySide)!;
  const { weekday, day, month } = dateTile(match.startsAt, tz);
  const checkIn = new Date(Date.parse(match.startsAt) - CHECK_IN_MINUTES * 60000).toISOString();
  const directions = directionsUrl(match.venue);

  return (
    <>
      <section
        aria-label="Next match"
        className="flex flex-col gap-4 rounded-3xl bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0] dark:border dark:border-[#30342a] dark:bg-[#1c1f18]"
      >
        <div className="flex items-center justify-between">
          <span className="flex items-center gap-1.5 rounded-full bg-white/10 px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-lime">
            <CheckIcon className="size-3" strokeWidth={3} />
            MATCH SCHEDULED
          </span>
          <span className="text-[11px] font-semibold tracking-[0.12em] text-[#a3a596] uppercase">
            {weekday} {day} {month}
          </span>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
          <SideBlock side={mine} meId={meId} />
          <span className="text-[22px] font-semibold tracking-[-0.02em] text-[#a3a596]">VS</span>
          <SideBlock side={theirs} meId={meId} />
        </div>
        <div className="grid grid-cols-3 rounded-2xl bg-white/10 px-1 py-3">
          {[
            [clockTime(match.startsAt, tz), "START"],
            [match.court.label, "COURT"],
            [formatName(match.format), "FORMAT"],
          ].map(([value, label], i) => (
            <div key={label} className={`flex flex-col items-center gap-0.5 ${i ? "border-l border-white/10" : ""}`}>
              <span className="text-base font-semibold tracking-[-0.02em]">{value}</span>
              <span className="text-[10px] font-semibold tracking-[0.12em] text-[#a3a596]">{label}</span>
            </div>
          ))}
        </div>
      </section>

      <div className="flex items-center gap-3 rounded-[20px] border border-line bg-surface px-3.5 py-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-background">
          <PinIcon className="size-[19px]" />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-[15px] font-semibold">{match.venue.name}</span>
          <span className="truncate text-[13px] text-muted">
            Check in by {clockTime(checkIn, tz)}
            {match.judge && ` · Judge ${match.judge.name}`}
          </span>
        </span>
        {directions && (
          <a href={directions} target="_blank" rel="noreferrer" className="flex min-h-11 items-center text-[13px] font-semibold">
            Directions
          </a>
        )}
      </div>
    </>
  );
}

function MatchRow({ match, meId }: { match: MyMatch; meId: string }) {
  const tz = match.venue.timezone;
  const mine = match.sides.find((s) => s.side === match.mySide)!;
  const theirs = match.sides.find((s) => s.side !== match.mySide)!;
  const result =
    match.status === "completed" && match.winnerSide
      ? match.winnerSide === match.mySide
        ? "Won"
        : "Lost"
      : match.status === "walkover"
        ? "Walkover"
        : match.status === "cancelled"
          ? "Cancelled"
          : clockTime(match.startsAt, tz);
  return (
    <div className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3">
      <DateTile iso={match.startsAt} timeZone={tz} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[15px] font-semibold">
          {sideNames(mine, meId)} vs {sideNames(theirs, meId)}
        </span>
        <span className="truncate text-xs text-muted">
          {match.event?.title ?? match.venue.name} · {match.court.label} · {formatName(match.format)}
        </span>
      </span>
      <span className="shrink-0 text-[13px] font-semibold">{result}</span>
    </div>
  );
}

function WaitingRow({ entry }: { entry: Entry }) {
  const tz = entry.event.venue.timezone;
  const held = entry.status === "pending_payment" && !entry.holdExpired;
  const [label, dot] =
    entry.status === "standby" ? ["Standby", "bg-[#f5a524]"] : held ? ["Pay to confirm", "bg-danger"] : ["Pairing", "bg-[#f5a524]"];
  return (
    <Link href={`/entries/${entry.id}`} className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3">
      <DateTile iso={entry.event.startsAt} timeZone={tz} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[15px] font-semibold">{entry.event.title}</span>
        <span className="truncate text-xs text-muted">
          {entry.event.venue.name} · {held ? rupees(entry.event.feePaise) : `paid ${rupees(entry.event.feePaise)}`}
        </span>
      </span>
      <span className="flex shrink-0 items-center gap-1.5 rounded-full bg-background px-2.5 py-1.5 text-xs font-semibold text-muted">
        <span className={`size-[7px] rounded-full ${dot}`} />
        {label}
      </span>
    </Link>
  );
}

type Tab = "upcoming" | "history";

function MatchesView({ user }: { user: User }) {
  const [tab, setTab] = useState<Tab>("upcoming");
  const [data, setData] = useState<{ matches: MyMatch[]; entries: Entry[] } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([myMatches(), myEntries()])
      .then(([matches, entries]) => setData({ matches, entries }))
      .catch((err) => setError(err instanceof ApiError ? err.message : "Couldn't load your matches."));
  }, []);

  const upcoming = data?.matches.filter((m) => ["scheduled", "ready", "live"].includes(m.status)) ?? [];
  const history = data?.matches.filter((m) => !["scheduled", "ready", "live"].includes(m.status)).reverse() ?? [];
  // Paid but not yet in a match, or held while paying.
  const waiting =
    data?.entries.filter(
      (e) =>
        e.status === "confirmed" ||
        e.status === "standby" ||
        (e.status === "pending_payment" && !e.holdExpired),
    ) ?? [];
  const [next, ...later] = upcoming;

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col gap-4 px-5 pt-6 pb-36">
        <header className="flex flex-col gap-1.5">
          <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">Your games</p>
          <h1 className="text-[32px] leading-tight font-medium tracking-[-0.04em]">Matches</h1>
        </header>

        <div role="tablist" aria-label="Matches" className="grid grid-cols-2 gap-1 rounded-2xl bg-line/60 p-1">
          {(["upcoming", "history"] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={tab === t}
              onClick={() => setTab(t)}
              className={`min-h-11 rounded-xl text-sm font-semibold capitalize ${tab === t ? "bg-surface text-foreground shadow-sm" : "text-muted"}`}
            >
              {t}
            </button>
          ))}
        </div>

        {error && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        )}
        {!data && !error && <div className="h-[260px] animate-pulse rounded-3xl bg-line" aria-busy="true" />}

        {data && tab === "upcoming" && (
          <>
            {next ? (
              <NextMatch match={next} meId={user.id} />
            ) : (
              <div className="flex flex-col gap-1 rounded-[20px] border border-line bg-surface p-5">
                <p className="text-[15px] font-semibold">No matches scheduled yet</p>
                <p className="text-sm text-muted">
                  Join an event and we&rsquo;ll pair you before it starts.{" "}
                  <Link href="/dashboard" className="font-semibold text-foreground underline underline-offset-4">
                    Find an event
                  </Link>
                </p>
              </div>
            )}
            {later.length > 0 && (
              <section className="flex flex-col gap-2.5">
                <h2 className="text-[17px] font-semibold tracking-[-0.02em]">Later</h2>
                {later.map((m) => (
                  <MatchRow key={m.id} match={m} meId={user.id} />
                ))}
              </section>
            )}
            {waiting.length > 0 && (
              <section className="flex flex-col gap-2.5">
                <h2 className="text-[17px] font-semibold tracking-[-0.02em]">Waiting</h2>
                {waiting.map((e) => (
                  <WaitingRow key={e.id} entry={e} />
                ))}
              </section>
            )}
          </>
        )}

        {data && tab === "history" && (
          <section className="flex flex-col gap-2.5">
            {history.length === 0 ? (
              <p className="rounded-[20px] border border-line bg-surface p-5 text-sm text-muted">
                Your finished matches and results show up here.
              </p>
            ) : (
              history.map((m) => <MatchRow key={m.id} match={m} meId={user.id} />)
            )}
          </section>
        )}
      </div>
      <BottomNav active="matches" />
    </main>
  );
}

/** The player's matches: the next one up front, then later ones and entries still waiting. */
export default function MatchesPage() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    me()
      .then((u) => {
        if (!u.profileComplete) {
          router.replace("/setup-profile");
          return;
        }
        setUser(u);
      })
      .catch(() => router.replace("/login"));
  }, [router]);

  if (!user) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }
  return <MatchesView user={user} />;
}
