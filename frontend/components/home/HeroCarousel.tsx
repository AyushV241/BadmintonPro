"use client";

import Link from "next/link";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { formatName } from "@/components/events/EventBits";
import { LiveDot, SideAvatars, headlinesGames, sideName, sideOf } from "@/components/live/LiveBits";
import { watchMatch, type LiveMatch, type MyMatch } from "@/lib/api";
import { clockTime, dateTile } from "@/lib/eventTime";

export type Slide = { key: string; label: string; node: ReactNode };

const cardClass =
  "relative flex h-[230px] flex-col justify-between overflow-hidden rounded-[26px] bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0]";

/** The home page's top cards, swiped sideways, with dots for the page. */
export function HeroCarousel({ slides }: { slides: Slide[] }) {
  const track = useRef<HTMLDivElement>(null);
  const [index, setIndex] = useState(0);

  if (slides.length === 1) return <>{slides[0].node}</>;

  return (
    <section aria-roledescription="carousel" aria-label="Your matches and events" className="flex flex-col gap-2.5">
      <div
        ref={track}
        onScroll={(e) => {
          const el = e.currentTarget;
          setIndex(Math.round(el.scrollLeft / el.clientWidth));
        }}
        className="-mx-5 flex snap-x snap-mandatory gap-3 overflow-x-auto scroll-smooth px-5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {slides.map((s, i) => (
          <div
            key={s.key}
            role="group"
            aria-roledescription="slide"
            aria-label={`${i + 1} of ${slides.length}: ${s.label}`}
            className="w-full shrink-0 snap-center"
          >
            {s.node}
          </div>
        ))}
      </div>
      <div className="flex justify-center gap-1.5">
        {slides.map((s, i) => (
          <button
            key={s.key}
            type="button"
            aria-label={`Show ${s.label}`}
            aria-current={i === index}
            onClick={() => {
              const el = track.current;
              el?.scrollTo({ left: i * el.clientWidth, behavior: "smooth" });
            }}
            className="flex size-6 items-center justify-center"
          >
            <span className={`h-1.5 rounded-full transition-all ${i === index ? "w-5 bg-ink" : "w-1.5 bg-line"}`} />
          </button>
        ))}
      </div>
    </section>
  );
}

/** A match being played now, with its score as it changes. */
export function LiveSlide({ match, meId }: { match: MyMatch; meId: string }) {
  const [live, setLive] = useState<LiveMatch | null>(null);
  useEffect(() => watchMatch(match.id, setLive), [match.id]);

  const games = live?.score.games ?? [];
  const cur = games[games.length - 1] ?? { a: 0, b: 0 };
  const a = sideOf(match.sides, "A");
  const b = sideOf(match.sides, "B");
  const mine = match.mySide;
  const over = live && live.status !== "live";
  const showGames = over && headlinesGames(live.score);

  return (
    <Link href={`/matches/${encodeURIComponent(match.id)}/live`} className={cardClass} aria-label="Your live match, watch it">
      <div className="flex items-center justify-between gap-2 text-[11px] font-semibold tracking-[0.12em]">
        <span className="flex items-center gap-1.5 rounded-full bg-[#262920] px-2.5 py-1.5">
          {!over && <LiveDot />}
          {over ? (live.status === "completed" ? "FINAL" : live.status.toUpperCase()) : `LIVE · GAME ${Math.max(1, games.length)}`}
        </span>
        <span className="truncate text-[#a3a596] uppercase">{match.court.label}</span>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
        <div className="flex min-w-0 flex-col items-center gap-1.5">
          <SideAvatars side={a} highlight={mine === "A"} size={40} />
          <span className="max-w-full truncate text-sm font-semibold">{sideName(a, meId)}</span>
        </div>
        {/* Once it's over, the games won; until then, the game in play. */}
        <span className="flex items-center gap-1.5 text-[48px] leading-none font-bold tracking-[-0.05em] tabular-nums">
          <span className={mine === "A" ? "text-lime" : ""}>{showGames ? live.score.gamesA : cur.a}</span>
          <span className="text-[30px] text-[#a3a596]">:</span>
          <span className={mine === "B" ? "text-lime" : ""}>{showGames ? live.score.gamesB : cur.b}</span>
        </span>
        <div className="flex min-w-0 flex-col items-center gap-1.5">
          <SideAvatars side={b} highlight={mine === "B"} size={40} />
          <span className="max-w-full truncate text-sm font-semibold">{sideName(b, meId)}</span>
        </div>
      </div>
      <span className="flex min-h-11 items-center justify-center rounded-2xl bg-lime text-sm font-semibold text-ink">Watch live</span>
    </Link>
  );
}

/** The player's next match: who, when and where. */
export function FixtureSlide({ match, meId }: { match: MyMatch; meId: string }) {
  const tz = match.venue.timezone;
  const { weekday, day, month } = dateTile(match.startsAt, tz);
  const mine = sideOf(match.sides, match.mySide);
  const theirs = match.sides.find((s) => s.side !== match.mySide);
  return (
    <Link href={`/matches/${encodeURIComponent(match.id)}/live`} className={cardClass} aria-label="Your next match">
      <div className="flex items-center justify-between text-[11px] font-semibold tracking-[0.12em]">
        <span className="rounded-full bg-[#262920] px-2.5 py-1.5 text-lime">YOUR NEXT MATCH</span>
        <span className="text-[#a3a596] uppercase">
          {weekday} {day} {month}
        </span>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
        <div className="flex min-w-0 flex-col items-center gap-1.5">
          <SideAvatars side={mine} highlight size={44} />
          <span className="max-w-full truncate text-sm font-semibold">{sideName(mine, meId)}</span>
        </div>
        <span className="text-xl font-semibold text-[#a3a596]">VS</span>
        <div className="flex min-w-0 flex-col items-center gap-1.5">
          <SideAvatars side={theirs} size={44} />
          <span className="max-w-full truncate text-sm font-semibold">{sideName(theirs, meId)}</span>
        </div>
      </div>
      <div className="grid grid-cols-3 rounded-2xl bg-[#262920] px-1 py-2.5">
        {[
          [clockTime(match.startsAt, tz), "START"],
          [match.court.label, "COURT"],
          [formatName(match.format), "FORMAT"],
        ].map(([value, label], i) => (
          <div key={label} className={`flex flex-col items-center gap-0.5 ${i ? "border-l border-[#33372c]" : ""}`}>
            <span className="text-[15px] font-semibold tracking-[-0.02em]">{value}</span>
            <span className="text-[10px] font-semibold tracking-[0.12em] text-[#a3a596]">{label}</span>
          </div>
        ))}
      </div>
    </Link>
  );
}
