"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { BottomNav } from "@/components/app/BottomNav";
import { EventRow, NextEventCard, joinable, myStatusIn } from "@/components/events/EventBits";
import { ArrowRightIcon, ShieldIcon } from "@/components/icons";
import { useAdminMode } from "@/lib/adminMode";
import { me, myEntries, myMatches, nearbyPlayers, type Entry, type MyMatch, type Player, type User } from "@/lib/api";
import { currentPosition } from "@/lib/location";
import { initials } from "@/lib/initials";
import { STARTING_RATING } from "@/lib/rating";
import { useNearbyEvents } from "@/lib/useNearbyEvents";

/** "Sunday, 4 October", in the viewer's own time zone. */
function longDate(date: Date) {
  return new Intl.DateTimeFormat("en-GB", { weekday: "long", day: "numeric", month: "long" }).format(date);
}

function Eyebrow({ children, className = "" }: { children: string; className?: string }) {
  return (
    <p className={`text-[11px] font-semibold tracking-[0.14em] text-muted uppercase ${className}`}>{children}</p>
  );
}

/** The player's rating, where the weekly chart used to be. */
function RatingStrip() {
  // TODO(step 6): the real rating, this week's change and the city rank.
  return (
    <div className="flex items-center gap-3 rounded-[20px] border border-line bg-surface py-3 pr-3 pl-4">
      <div className="flex flex-1 flex-col gap-0.5">
        <Eyebrow>Your rating · Singles</Eyebrow>
        <span className="text-[26px] leading-tight font-semibold tracking-[-0.04em]">{STARTING_RATING}</span>
      </div>
      <span className="rounded-full bg-lime/30 px-2.5 py-1.5 text-xs font-semibold text-lime-text">
        New player
      </span>
    </div>
  );
}

/** Up to three players to challenge, active at venues near the player. */
function ChallengeNearby({ fallbackLat, fallbackLng }: { fallbackLat: number | null; fallbackLng: number | null }) {
  const [players, setPlayers] = useState<Player[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    currentPosition()
      .then((p) => ({ lat: p.coords.latitude, lng: p.coords.longitude }))
      // No location: the nearest event's venue stands in for "near you".
      .catch(() => (fallbackLat !== null && fallbackLng !== null ? { lat: fallbackLat, lng: fallbackLng } : null))
      .then((point) => (point ? nearbyPlayers(point.lat, point.lng) : []))
      .then((list) => !cancelled && setPlayers(list.slice(0, 3)))
      .catch(() => !cancelled && setPlayers([]));
    return () => {
      cancelled = true;
    };
  }, [fallbackLat, fallbackLng]);

  if (!players || players.length === 0) return null;
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold tracking-[-0.02em]">Challenge people near you</h2>
        <Link href="/challenge/new" className="flex min-h-11 items-center text-[13px] font-semibold text-muted">
          See all
        </Link>
      </div>
      <div className="grid grid-cols-3 gap-2">
        {players.map((p) => (
          <Link
            key={p.id}
            href={`/challenge/new?username=${encodeURIComponent(p.username)}`}
            aria-label={`Challenge ${p.name}, rated ${p.rating}`}
            className="flex flex-col items-center gap-1.5 rounded-[20px] border border-line bg-surface px-2 py-3"
          >
            <Avatar text={initials(p.name, p.username)} color={avatarColors.blue} size={36} />
            <span className="flex max-w-full flex-col items-center">
              <span className="max-w-full truncate text-sm font-semibold">{p.name.split(" ")[0] || p.username}</span>
              <span className="text-xs text-muted">{p.rating}</span>
            </span>
            <span className="flex min-h-7 items-center rounded-full bg-primary px-3 text-xs font-semibold text-on-primary">
              Challenge
            </span>
          </Link>
        ))}
      </div>
    </section>
  );
}

function NearbyEvents() {
  const nearby = useNearbyEvents();
  // What the player has already joined, so cards say "You're in" instead of Join.
  const [mine, setMine] = useState<{ entries: Entry[]; matches: MyMatch[] }>({ entries: [], matches: [] });
  useEffect(() => {
    Promise.all([myEntries(), myMatches()])
      .then(([entries, matches]) => setMine({ entries, matches }))
      .catch(() => {
        /* cards just offer Join */
      });
  }, []);
  const statusOf = (eventId: string) => myStatusIn(eventId, mine.entries, mine.matches);

  if (nearby.state === "loading") {
    return (
      <div className="flex flex-col gap-3" aria-busy="true" aria-label="Loading events">
        <div className="h-[230px] animate-pulse rounded-[26px] bg-line" />
        <div className="h-[82px] animate-pulse rounded-[20px] bg-line" />
      </div>
    );
  }
  if (nearby.state === "error") {
    return (
      <p role="alert" className="rounded-[20px] border border-line bg-surface p-4 text-sm text-muted">
        {nearby.message}
      </p>
    );
  }
  // The photo card offers Join, so it goes to the first event still open.
  const next = nearby.events.find((e) => joinable(e));
  const more = nearby.events.filter((e) => e !== next);
  if (!next) {
    return (
      <div className="flex flex-col gap-1 rounded-[20px] border border-line bg-surface p-5">
        <p className="text-[15px] font-semibold">No events open near you</p>
        <p className="text-sm text-muted">New events are added every week. Check back soon.</p>
      </div>
    );
  }
  return (
    <>
      <NextEventCard event={next} mine={statusOf(next.id)} />
      <RatingStrip />
      <ChallengeNearby fallbackLat={next.venue.lat} fallbackLng={next.venue.lng} />
      {more.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="text-lg font-semibold tracking-[-0.02em]">
            {nearby.byDistance ? "Events near you" : "Events in Bangalore"}
          </h2>
          <div className="flex flex-col gap-3">
            {more.map((e) => (
              <EventRow key={e.id} event={e} mine={statusOf(e.id)} />
            ))}
          </div>
        </section>
      )}
    </>
  );
}

/** Home: the screen you land on after signing in. */
export default function HomePage() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const adminMode = useAdminMode();

  useEffect(() => {
    // The session cookie is httpOnly, so the only way to know whether we are
    // signed in is to ask the server.
    me()
      .then((u) => {
        // The profile step comes first.
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

  const firstName = user.name.trim().split(/\s+/)[0] || user.username;
  const avatarText = initials(user.name, user.username);

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto w-full max-w-[480px] pb-36">
        <header className="flex items-end justify-between gap-3 px-5 pt-6 pb-5">
          <div className="flex flex-col gap-1.5">
            <Eyebrow>{longDate(new Date())}</Eyebrow>
            <h1 className="text-[32px] leading-tight font-medium tracking-[-0.04em]">
              Hey, <span className="font-semibold">{firstName}</span>
            </h1>
          </div>
          <Link href="/profile" aria-label="Your profile" className="rounded-full">
            <Avatar text={avatarText} color={avatarColors.peach} size={44} />
          </Link>
        </header>

        <div className="flex flex-col gap-5 px-5">
          {adminMode && (
            <Link
              href="/admin"
              className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-4"
            >
              <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-lime text-ink">
                <ShieldIcon className="size-5" />
              </span>
              <span className="flex flex-1 flex-col">
                <span className="text-[15px] font-semibold tracking-[-0.01em]">Admin mode is on</span>
                <span className="text-xs text-muted">Manage venues, events and judges</span>
              </span>
              <ArrowRightIcon className="size-5 text-muted" />
            </Link>
          )}
          <NearbyEvents />
        </div>
      </div>

      <BottomNav active="home" />

    </main>
  );
}
