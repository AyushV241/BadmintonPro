"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { DateTile, formatName } from "@/components/events/EventBits";
import { ShieldIcon } from "@/components/icons";
import { Button } from "@/components/ui";
import {
  ApiError,
  judgeHome,
  logout,
  meSession,
  setSessionMode,
  type JudgeEvent,
  type JudgeHome,
  type JudgeMatch,
  type MatchSide,
  type MatchStatus,
} from "@/lib/api";
import { clockTime, dateTile } from "@/lib/eventTime";
import { initials } from "@/lib/initials";

/** "Sunday, 4 October", in the viewer's own time zone. */
function longDate(date: Date) {
  return new Intl.DateTimeFormat("en-GB", { weekday: "long", day: "numeric", month: "long" }).format(date);
}

function Eyebrow({ children }: { children: string }) {
  return <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">{children}</p>;
}

const statusLabels: Record<MatchStatus, string> = {
  scheduled: "Scheduled",
  ready: "Ready",
  live: "Live",
  completed: "Finished",
  retired: "Retired",
  walkover: "Walkover",
  cancelled: "Cancelled",
  void: "Void",
};

function sideNames(side: MatchSide | undefined) {
  return side ? side.players.map((p) => p.name.split(" ")[0]).join(" & ") : "TBC";
}

function sides(match: JudgeMatch) {
  return [match.sides.find((s) => s.side === "A"), match.sides.find((s) => s.side === "B")] as const;
}

function SideBlock({ side }: { side: MatchSide | undefined }) {
  return (
    <div className="flex min-w-0 flex-col items-center gap-1.5 text-center">
      <span className="flex">
        {side?.players.map((p, i) => (
          <span key={p.userId} className={i ? "-ml-3" : ""}>
            <Avatar text={initials(p.name, p.username)} color={avatarColors.sand} size={side.players.length > 1 ? 46 : 58} />
          </span>
        ))}
      </span>
      <span className="max-w-full truncate text-[15px] font-semibold">{sideNames(side)}</span>
      {side && <span className="text-xs text-[#a3a596]">Rating {side.rating}</span>}
    </div>
  );
}

/** The judge's next match, in the same card style as a player's. */
function NextMatch({ match }: { match: JudgeMatch }) {
  const tz = match.venue.timezone;
  const [a, b] = sides(match);
  const { weekday, day, month } = dateTile(match.startsAt, tz);
  return (
    <section
      aria-label="Next match to judge"
      className="flex flex-col gap-4 rounded-3xl bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0] border border-transparent dark:border-[#30342a] dark:bg-[#1c1f18]"
    >
      <div className="flex items-center justify-between">
        <span className="rounded-full bg-white/10 px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-lime">
          YOU&apos;RE JUDGING
        </span>
        <span className="text-[11px] font-semibold tracking-[0.12em] text-[#a3a596] uppercase">
          {weekday} {day} {month}
        </span>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
        <SideBlock side={a} />
        <span className="text-[22px] font-semibold tracking-[-0.02em] text-[#a3a596]">VS</span>
        <SideBlock side={b} />
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
      <p className="text-[13px] text-[#a3a596]">
        {match.event?.title ?? match.venue.name} · {match.venue.name}
      </p>
      <Link
        href={`/judge/matches/${encodeURIComponent(match.id)}`}
        className="flex min-h-[52px] items-center justify-center rounded-2xl bg-lime text-base font-semibold text-ink"
      >
        {match.status === "live" ? "Continue scoring" : "Start match"}
      </Link>
    </section>
  );
}

function MatchRow({ match }: { match: JudgeMatch }) {
  const tz = match.venue.timezone;
  const [a, b] = sides(match);
  const upcoming = match.status === "scheduled" || match.status === "ready";
  return (
    <Link
      href={`/judge/matches/${encodeURIComponent(match.id)}`}
      className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3"
    >
      <DateTile iso={match.startsAt} timeZone={tz} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[15px] font-semibold">
          {sideNames(a)} vs {sideNames(b)}
        </span>
        <span className="truncate text-xs text-muted">
          {match.event?.title ?? match.venue.name} · {match.court.label} · {formatName(match.format)}
        </span>
      </span>
      <span className="shrink-0 text-[13px] font-semibold">
        {upcoming ? clockTime(match.startsAt, tz) : statusLabels[match.status]}
      </span>
    </Link>
  );
}

function EventItem({ event }: { event: JudgeEvent }) {
  const tz = event.venue.timezone;
  return (
    <div className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3">
      <DateTile iso={event.startsAt} timeZone={tz} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[15px] font-semibold">{event.title}</span>
        <span className="truncate text-xs text-muted">
          {event.venue.name} · {clockTime(event.startsAt, tz)}–{clockTime(event.endsAt, tz)}
        </span>
      </span>
      {event.status === "draft" && <span className="shrink-0 text-xs text-muted">Draft</span>}
    </div>
  );
}

/** The judge home page: where "Log in as judge" lands. */
export default function JudgePage() {
  const router = useRouter();
  const [home, setHome] = useState<JudgeHome | null>(null);
  const [notJudge, setNotJudge] = useState(false);
  const [busy, setBusy] = useState<"player" | "logout" | null>(null);

  useEffect(() => {
    let active = true;
    meSession()
      .then(async (s) => {
        if (!s.isJudge) {
          if (active) setNotJudge(true);
          return;
        }
        // A judge who opened /judge from the player view lands in judge mode.
        if (s.mode !== "judge") await setSessionMode("judge");
        const h = await judgeHome();
        if (active) setHome(h);
      })
      .catch((err) => {
        if (!active) return;
        if (err instanceof ApiError && err.status === 403) setNotJudge(true);
        else router.replace("/login");
      });
    return () => {
      active = false;
    };
  }, [router]);

  async function playAsPlayer() {
    setBusy("player");
    try {
      const s = await setSessionMode("player");
      router.push(s.user.profileComplete ? "/dashboard" : "/setup-profile");
    } catch {
      setBusy(null);
    }
  }

  async function signOut() {
    setBusy("logout");
    await logout().catch(() => {});
    router.replace("/login");
  }

  if (notJudge) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background px-6">
        <div className="flex max-w-sm flex-col items-center gap-3 text-center">
          <h1 className="text-xl font-semibold">You&apos;re not a registered judge</h1>
          <p className="text-sm text-muted">
            Ask your event admin to add your phone number under Judges, then log in as judge again.
          </p>
          <Button variant="secondary" onClick={() => router.replace("/dashboard")}>
            Go to the player home
          </Button>
        </div>
      </main>
    );
  }

  if (!home) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  const upcoming = home.matches.filter((m) => m.status === "scheduled" || m.status === "ready" || m.status === "live");
  const finished = home.matches.filter((m) => !upcoming.includes(m));
  const [next, ...later] = upcoming;
  const firstName = home.judge.name.trim().split(/\s+/)[0];

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col gap-6 px-5 pt-6 pb-16">
        <header className="flex items-end justify-between gap-3">
          <div className="flex flex-col gap-1.5">
            <Eyebrow>{longDate(new Date())}</Eyebrow>
            <h1 className="text-[32px] leading-tight font-medium tracking-[-0.04em]">
              Hey, <span className="font-semibold">{firstName}</span>
            </h1>
          </div>
          <span className="flex items-center gap-1.5 rounded-full bg-lime px-3 py-1.5 text-xs font-semibold text-ink">
            <ShieldIcon className="size-3.5" />
            Judge
          </span>
        </header>

        <section className="flex flex-col gap-3" aria-labelledby="judge-matches">
          <h2 id="judge-matches" className="text-[17px] font-semibold tracking-[-0.02em]">
            Matches to judge
          </h2>
          {next ? (
            <>
              <NextMatch match={next} />
              {later.map((m) => (
                <MatchRow key={m.id} match={m} />
              ))}
            </>
          ) : (
            <p className="rounded-[20px] border border-line bg-surface p-4 text-sm text-muted">
              No matches assigned yet. They appear here once an event&apos;s schedule is published.
            </p>
          )}
          {finished.length > 0 && (
            <>
              <Eyebrow>Finished today</Eyebrow>
              {finished.map((m) => (
                <MatchRow key={m.id} match={m} />
              ))}
            </>
          )}
        </section>

        <section className="flex flex-col gap-3" aria-labelledby="judge-events">
          <h2 id="judge-events" className="text-[17px] font-semibold tracking-[-0.02em]">
            Events you&apos;re judging
          </h2>
          {home.events.length > 0 ? (
            home.events.map((e) => <EventItem key={e.id} event={e} />)
          ) : (
            <p className="rounded-[20px] border border-line bg-surface p-4 text-sm text-muted">
              You&apos;re not on any upcoming event&apos;s judge list.
            </p>
          )}
        </section>

        <div className="flex flex-col gap-2">
          <Button variant="secondary" fullWidth loading={busy === "player"} onClick={playAsPlayer}>
            Play as a player
          </Button>
          <Button variant="ghost" fullWidth className="text-muted" loading={busy === "logout"} onClick={signOut}>
            Sign out
          </Button>
        </div>
      </div>
    </main>
  );
}
