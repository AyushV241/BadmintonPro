"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useState } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { joinable } from "@/components/events/EventBits";
import { PlayerSearch } from "@/components/events/PlayerSearch";
import { FieldError } from "@/components/form/FieldError";
import { ChevronLeftIcon } from "@/components/icons";
import {
  ApiError,
  me,
  recentPlayers,
  searchUsers,
  sendChallenge,
  type BadmintonEvent,
  type Player,
  type PublicUser,
} from "@/lib/api";
import { clockTime, dateTile, rupees } from "@/lib/eventTime";
import { initials } from "@/lib/initials";
import { useFormErrors } from "@/lib/useFormErrors";
import { useNearbyEvents } from "@/lib/useNearbyEvents";

/** Most preferred-time choices to offer for one event. */
const MAX_TIMES = 6;

function Header({ back, eyebrow, title, onBack }: { back?: string; eyebrow: string; title: string; onBack?: () => void }) {
  const icon = <ChevronLeftIcon className="size-[22px]" />;
  return (
    <header className="flex items-center gap-1.5 px-3 pt-3.5 pb-1.5">
      {onBack ? (
        <button type="button" onClick={onBack} aria-label="Back" className="flex size-11 items-center justify-center">
          {icon}
        </button>
      ) : (
        <Link href={back ?? "/dashboard"} aria-label="Back" className="flex size-11 items-center justify-center">
          {icon}
        </Link>
      )}
      <div className="flex flex-col gap-0.5">
        <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">{eyebrow}</p>
        <h1 className="text-2xl font-semibold tracking-[-0.04em]">{title}</h1>
      </div>
    </header>
  );
}

function Eyebrow({ children }: { children: string }) {
  return <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">{children}</p>;
}

const optionClass = (on: boolean) =>
  `min-h-11 rounded-xl px-3.5 text-sm font-semibold ${on ? "bg-primary text-on-primary" : "border border-line bg-surface"}`;

function PickPlayer({ onPick }: { onPick: (p: PublicUser | Player) => void }) {
  const [recent, setRecent] = useState<Player[] | null>(null);
  useEffect(() => {
    recentPlayers()
      .then(setRecent)
      .catch(() => setRecent([]));
  }, []);

  return (
    <div className="flex flex-col gap-5 px-5 pt-2">
      <PlayerSearch id="challenge-search" label="Search by username" onPick={onPick} />
      {recent && recent.length > 0 && (
        <section className="flex flex-col gap-2">
          <Eyebrow>Recent players</Eyebrow>
          <ul className="overflow-hidden rounded-[20px] border border-line bg-surface">
            {recent.map((p) => (
              <li key={p.id} className="border-b border-line last:border-b-0">
                <button type="button" onClick={() => onPick(p)} className="flex min-h-16 w-full items-center gap-3 px-3.5 py-2 text-left">
                  <Avatar text={initials(p.name, p.username)} color={avatarColors.blue} size={44} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-[15px] font-semibold">{p.name}</span>
                    <span className="truncate text-[13px] text-muted">@{p.username}</span>
                  </span>
                  <span className="flex flex-col items-end">
                    <span className="text-[15px] font-semibold">{p.rating}</span>
                    <span className="text-[10px] font-semibold tracking-[0.12em] text-muted">SINGLES</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

/** Start times inside the event's window, one singles slot apart. */
function startTimes(e: BadmintonEvent) {
  const slot = (e.slotMinutes.singles ?? 30) * 60_000;
  const start = Date.parse(e.startsAt);
  const last = Date.parse(e.endsAt) - slot;
  const times: string[] = [];
  for (let t = start; t <= last; t += slot) times.push(new Date(t).toISOString());
  // Spread the choices over the evening rather than offering only the first few.
  if (times.length <= MAX_TIMES) return times;
  const step = (times.length - 1) / (MAX_TIMES - 1);
  return Array.from({ length: MAX_TIMES }, (_, i) => times[Math.round(i * step)]);
}

function WhereAndWhen({
  opponent,
  initialEventId,
  onChangePlayer,
}: {
  opponent: PublicUser | Player;
  initialEventId: string | null;
  onChangePlayer: () => void;
}) {
  const router = useRouter();
  const nearby = useNearbyEvents();
  const events = useMemo(
    () => (nearby.state === "ready" ? nearby.events.filter((e) => joinable(e) && e.formats.includes("singles")) : []),
    [nearby],
  );
  // Venues in order of their first event, each with its events.
  const venues = useMemo(() => {
    const byVenue = new Map<string, { venue: BadmintonEvent["venue"]; km?: number; events: BadmintonEvent[] }>();
    for (const e of events) {
      const v = byVenue.get(e.venue.id) ?? { venue: e.venue, km: e.distanceKm, events: [] };
      v.events.push(e);
      byVenue.set(e.venue.id, v);
    }
    return [...byVenue.values()];
  }, [events]);

  const [picked, setPicked] = useState<{ eventId: string | null; time: string | null }>({
    eventId: initialEventId,
    time: null,
  });
  const event = events.find((e) => e.id === picked.eventId) ?? venues[0]?.events[0];
  const venue = venues.find((v) => v.venue.id === event?.venue.id);
  const times = event ? startTimes(event) : [];
  const time = picked.time && times.includes(picked.time) ? picked.time : (times[1] ?? times[0]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const { errors, field, show, clear } = useFormErrors<"venue" | "time">();

  async function send() {
    setError(null);
    if (!event) {
      // The venue list isn't on screen when there's nothing to pick.
      setError("There's no open singles event nearby to play the challenge at yet.");
      return;
    }
    if (show({ time: time ? undefined : "Pick a time." }) || !time) return;
    setBusy(true);
    try {
      const c = await sendChallenge(event.id, opponent.username, time);
      router.replace(`/challenges/${c.id}`);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Couldn't send the challenge.";
      const m = msg.toLowerCase();
      // Time and event problems show on those sections; who-can-play rules
      // (rating band, an open challenge already) are about the whole thing.
      if (m.includes("time") || m.includes("start")) show({ time: msg });
      else if (m.includes("full") || m.includes("closed") || m.includes("joining") || m.includes("singles")) show({ venue: msg });
      else setError(msg);
      setBusy(false);
    }
  }

  const first = opponent.name.split(" ")[0] || opponent.username;
  return (
    <>
      <div className="flex flex-col gap-4 px-5 pt-2">
        <div className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-2.5 pl-3">
          <Avatar text={initials(opponent.name, opponent.username)} color={avatarColors.blue} size={44} />
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="truncate text-[15px] font-semibold">{opponent.name}</span>
            <span className="truncate text-[13px] text-muted">
              @{opponent.username}
              {"rating" in opponent && ` · Singles ${opponent.rating}`}
            </span>
          </span>
          <button type="button" onClick={onChangePlayer} className="min-h-11 px-2.5 text-[13px] font-semibold underline underline-offset-[3px]">
            Change
          </button>
        </div>

        {nearby.state === "loading" && <div className="h-48 animate-pulse rounded-[20px] bg-line" aria-busy="true" />}
        {nearby.state !== "loading" && venues.length === 0 && (
          <p className="rounded-[20px] border border-line bg-surface p-4 text-sm text-muted">
            No singles events are open for joining nearby right now, so there&rsquo;s nowhere to play the challenge yet.
          </p>
        )}

        {venues.length > 0 && event && (
          <>
            <section {...field("venue")} className="flex flex-col gap-2">
              <Eyebrow>Venue with an upcoming event</Eyebrow>
              <div role="group" aria-label="Venue" className="overflow-hidden rounded-[20px] border border-line bg-surface">
                {venues.map((v) => {
                  const on = v.venue.id === event.venue.id;
                  return (
                    <button
                      key={v.venue.id}
                      type="button"
                      aria-pressed={on}
                      onClick={() => {
                        setPicked({ eventId: v.events[0].id, time: null });
                        clear("venue");
                        clear("time");
                      }}
                      className="flex min-h-16 w-full items-center gap-3 border-b border-line px-3.5 py-2.5 text-left last:border-b-0"
                    >
                      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span className="truncate text-[15px] font-semibold">{v.venue.name}</span>
                        <span className="truncate text-[13px] text-muted">
                          {v.km !== undefined && `${v.km.toFixed(1)} km · `}
                          {v.events.map((e) => e.title).join(", ")}
                        </span>
                      </span>
                      <span
                        aria-hidden="true"
                        className={`size-[22px] shrink-0 rounded-full ${on ? "border-[7px] border-foreground" : "border-2 border-line"}`}
                      />
                    </button>
                  );
                })}
              </div>
              <FieldError id="challenge-venue-error" message={errors.venue} />
            </section>

            <section className="flex flex-col gap-2">
              <Eyebrow>Date</Eyebrow>
              <div role="group" aria-label="Date" className="flex flex-wrap gap-2">
                {venue?.events.map((e) => {
                  const { weekday, day, month } = dateTile(e.startsAt, e.venue.timezone);
                  return (
                    <button
                      key={e.id}
                      type="button"
                      aria-pressed={e.id === event.id}
                      onClick={() => setPicked({ eventId: e.id, time: null })}
                      className={optionClass(e.id === event.id)}
                    >
                      {weekday} {day} {month}
                    </button>
                  );
                })}
              </div>
            </section>

            <section {...field("time")} className="flex flex-col gap-2">
              <Eyebrow>Preferred time</Eyebrow>
              <div role="group" aria-label="Preferred time" className="grid grid-cols-3 gap-2">
                {times.map((t) => (
                  <button
                    key={t}
                    type="button"
                    aria-pressed={t === time}
                    onClick={() => {
                      setPicked({ eventId: event.id, time: t });
                      clear("time");
                    }}
                    className={optionClass(t === time)}
                  >
                    {clockTime(t, event.venue.timezone)}
                  </button>
                ))}
              </div>
              <FieldError id="challenge-time-error" message={errors.time} />
            </section>

            <div className="flex items-center gap-2.5">
              <span className="rounded-full bg-line/60 px-3 py-1.5 text-[13px] font-semibold">Singles</span>
              <span className="text-xs text-muted">Doubles challenges are coming soon.</span>
            </div>
            <p className="rounded-2xl bg-line/60 px-3.5 py-3 text-[13px] leading-snug">
              {first} has 4 hours to accept, and you both pay <strong>{rupees(event.feePaise)}</strong> in that time. Once you&rsquo;ve
              both paid, we fix the free slot closest to your time, with a court and a judge.
            </p>
          </>
        )}
        {error && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        )}
      </div>

      <footer className="fixed bottom-0 left-1/2 z-10 flex w-full max-w-[480px] -translate-x-1/2 border-t border-line bg-surface px-5 pt-3 pb-[max(28px,env(safe-area-inset-bottom))]">
        <button
          type="button"
          onClick={() => void send()}
          disabled={busy}
          className="min-h-[54px] flex-1 rounded-2xl bg-primary text-base font-semibold text-on-primary disabled:opacity-50"
        >
          {busy ? "Sending…" : "Send challenge"}
        </button>
      </footer>
    </>
  );
}

function NewChallenge() {
  const router = useRouter();
  const params = useSearchParams();
  const eventId = params.get("event");
  const username = params.get("username");
  const [ready, setReady] = useState(false);
  const [opponent, setOpponent] = useState<PublicUser | Player | null>(null);

  useEffect(() => {
    me()
      .then(async (u) => {
        if (!u.profileComplete) {
          router.replace("/setup-profile");
          return;
        }
        // From "Challenge people near you": start with that player picked.
        if (username) {
          const users = await searchUsers(username).catch(() => []);
          const exact = users.find((x) => x.username === username.toLowerCase());
          if (exact) setOpponent(exact);
        }
        setReady(true);
      })
      .catch(() => router.replace("/login"));
  }, [router, username]);

  if (!ready) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto w-full max-w-[480px] pb-36">
        {opponent ? (
          <>
            <Header
              eyebrow={`Challenge ${opponent.name.split(" ")[0] || opponent.username}`}
              title="Where and when"
              onBack={() => setOpponent(null)}
            />
            <WhereAndWhen opponent={opponent} initialEventId={eventId} onChangePlayer={() => setOpponent(null)} />
          </>
        ) : (
          <>
            <Header back={eventId ? `/events/${eventId}` : "/dashboard"} eyebrow="Challenge" title="Pick a player" />
            <PickPlayer onPick={setOpponent} />
          </>
        )}
      </div>
    </main>
  );
}

/** Challenge a player: pick them, then a venue, date and time at an event. */
export default function NewChallengePage() {
  return (
    <Suspense>
      <NewChallenge />
    </Suspense>
  );
}
