"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { BottomNav } from "@/components/app/BottomNav";
import { ArrowRightIcon, ClockIcon, LogoutIcon, PinIcon, TrendUpIcon } from "@/components/icons";
import { BottomSheet, Button, useBottomSheet } from "@/components/ui";
import { logout, me, type User } from "@/lib/api";
import { initials } from "@/lib/initials";
import {
  avatarColors,
  sampleEvents,
  sampleMomentum,
  type EventSummary,
  type Player,
  type WeeklyMomentum,
} from "@/lib/mock/home";
import courtPhoto from "@/public/images/court-night.jpg";

// Event times are shown in the venue's time zone; every venue is in India for now.
const VENUE_TZ = "Asia/Kolkata";

function part(date: Date, options: Intl.DateTimeFormatOptions, type: Intl.DateTimeFormatPartTypes) {
  return new Intl.DateTimeFormat("en-GB", { timeZone: VENUE_TZ, ...options })
    .formatToParts(date)
    .find((p) => p.type === type)?.value ?? "";
}

/** "Sunday, 4 October" */
function longDate(date: Date) {
  return `${part(date, { weekday: "long" }, "weekday")}, ${part(date, { day: "numeric" }, "day")} ${part(date, { month: "long" }, "month")}`;
}

/** { clock: "7:30", period: "PM" } */
function timeParts(iso: string) {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: VENUE_TZ,
    hour: "numeric",
    minute: "2-digit",
  }).formatToParts(new Date(iso));
  const get = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)?.value ?? "";
  return { clock: `${get("hour")}:${get("minute")}`, period: get("dayPeriod") };
}

/** "7:30 PM" */
function time(iso: string) {
  const { clock, period } = timeParts(iso);
  return `${clock} ${period}`;
}

function Avatar({ text, color, size = 40, ring }: { text?: string; color: string; size?: number; ring?: boolean }) {
  return (
    <span
      className={`flex shrink-0 items-center justify-center rounded-full font-semibold tracking-[-0.02em] text-ink ${ring ? "ring-2 ring-surface" : ""}`}
      style={{ width: size, height: size, background: color, fontSize: Math.round(size * 0.36) }}
    >
      {text}
    </span>
  );
}

function AvatarStack({ players, size = 22 }: { players: Player[]; size?: number }) {
  return (
    <span className="flex">
      {players.map((p, i) => (
        <span key={i} className={i ? "-ml-2" : ""}>
          {/* Too small for readable initials: colour dots, as in the design. */}
          <Avatar color={p.color} size={size} ring />
        </span>
      ))}
    </span>
  );
}

function Eyebrow({ children, className = "" }: { children: string; className?: string }) {
  return (
    <p className={`text-[11px] font-semibold tracking-[0.14em] text-muted uppercase ${className}`}>{children}</p>
  );
}

function SectionHeader({ eyebrow, title }: { eyebrow: string; title: string }) {
  return (
    <div className="flex flex-col gap-1">
      <Eyebrow>{eyebrow}</Eyebrow>
      <h2 className="text-[22px] leading-tight font-semibold tracking-[-0.04em]">{title}</h2>
    </div>
  );
}

/** The photo card for the next event near you. */
function NextEventCard({ event }: { event: EventSummary }) {
  const words = event.title.split(" ");
  const last = words.pop();
  const day = part(new Date(event.startsAt), { weekday: "short" }, "weekday");

  return (
    // TODO: link to the event page once it exists.
    <article className="relative h-[230px] overflow-hidden rounded-[26px] bg-ink text-white">
      <Image
        src={courtPhoto}
        alt=""
        fill
        placeholder="blur"
        sizes="(max-width: 480px) 100vw, 440px"
        className="object-cover object-[60%_30%]"
      />
      <div
        aria-hidden="true"
        className="absolute inset-0 bg-[linear-gradient(90deg,rgba(21,23,18,0.85)_0%,rgba(21,23,18,0.4)_60%,rgba(21,23,18,0.15)_100%),linear-gradient(180deg,rgba(21,23,18,0)_45%,rgba(21,23,18,0.6)_100%)]"
      />
      <div className="absolute inset-0 flex flex-col justify-between p-[18px]">
        <span className="flex items-center gap-2 self-start rounded-full bg-ink/55 px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em]">
          <span className="size-[7px] rounded-full bg-lime shadow-[0_0_0_3px_rgba(226,251,108,0.25)]" />
          NEXT NEAR YOU · {event.spotsLeft} SPOTS LEFT
        </span>
        <div className="flex flex-col gap-1.5">
          <p className="text-[11px] font-semibold tracking-[0.14em] text-lime uppercase">
            Badminton · {event.format}
          </p>
          <h3 className="text-[28px] leading-[1.02] font-bold tracking-[-0.045em]">
            {words.join(" ")} <span className="text-lime">{last}</span>
          </h3>
          <div className="mt-1 flex items-center justify-between gap-2">
            <span className="flex items-center gap-3 text-[13px] text-white/80">
              <span className="flex items-center gap-1.5">
                <ClockIcon className="size-3.5" />
                {day} · {time(event.startsAt)}
              </span>
              <span className="flex items-center gap-1.5">
                <PinIcon className="size-3.5" />
                {event.distanceKm} km
              </span>
            </span>
            <span className="flex min-h-11 items-center gap-1.5 rounded-full bg-lime px-4 text-sm font-semibold text-ink">
              Join · ₹{event.feeInr}
              <ArrowRightIcon className="size-4" />
            </span>
          </div>
        </div>
      </div>
    </article>
  );
}

const DAYS = ["M", "T", "W", "T", "F", "S", "S"];

function MomentumCard({ momentum }: { momentum: WeeklyMomentum }) {
  const max = Math.max(...momentum.perDay, 1);
  const best = momentum.perDay.indexOf(Math.max(...momentum.perDay));

  return (
    <div className="flex flex-col gap-5 rounded-[22px] border border-line bg-surface p-5">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          <span className="flex items-baseline gap-1.5">
            <span className="text-[40px] leading-none font-semibold tracking-[-0.05em]">{momentum.matches}</span>
            <span className="text-sm text-muted">matches</span>
          </span>
          <span className="flex items-center gap-1 rounded-full bg-lime/30 px-2 py-1 text-xs font-semibold text-lime-text">
            <TrendUpIcon className="size-3.5" />+{momentum.ratingChange.toFixed(2)}
          </span>
        </div>
        <div className="flex flex-col items-end">
          <span className="text-[22px] leading-none font-semibold tracking-[-0.03em]">{momentum.rating.toFixed(1)}</span>
          <span className="mt-1 text-xs text-muted">Rating · {momentum.wins} wins</span>
        </div>
      </div>
      <div
        role="img"
        aria-label={`Matches per day this week: ${momentum.perDay.join(", ")}`}
        className="grid grid-cols-7 items-end gap-3"
      >
        {momentum.perDay.map((n, i) => (
          <span key={i} className="flex flex-col items-center gap-2">
            <span className="flex h-[72px] w-full items-end">
              <span
                className={`w-full rounded-lg ${i === best ? "bg-lime" : "bg-line"}`}
                style={{ height: `${Math.max(12, (n / max) * 100)}%` }}
              />
            </span>
            <span className={`text-[11px] ${i === best ? "font-semibold text-foreground" : "text-muted"}`}>
              {DAYS[i]}
            </span>
          </span>
        ))}
      </div>
    </div>
  );
}

function EventRow({ event }: { event: EventSummary }) {
  const start = new Date(event.startsAt);
  return (
    // TODO: link to the event page once it exists.
    <article className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3">
      <span className="flex h-14 w-[52px] shrink-0 flex-col items-center justify-center rounded-[14px] bg-background">
        <span className="text-xl leading-none font-semibold tracking-[-0.03em]">
          {part(start, { day: "numeric" }, "day")}
        </span>
        <span className="mt-1 text-[10px] font-semibold tracking-[0.12em] text-muted uppercase">
          {part(start, { month: "short" }, "month")}
        </span>
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="text-[10px] font-semibold tracking-[0.12em] text-muted uppercase">
          {part(start, { weekday: "short" }, "weekday")} · {event.format}
        </p>
        <h3 className="truncate text-[15px] font-semibold tracking-[-0.01em]">{event.title}</h3>
        <p className="flex items-center gap-1 truncate text-xs text-muted">
          <PinIcon className="size-3 shrink-0" />
          {event.venue} · {event.distanceKm} km
        </p>
        <span className="flex items-center gap-2">
          <AvatarStack players={event.players} />
          <span className="text-xs font-semibold text-lime-text">{event.spotsLeft} spots left</span>
        </span>
      </div>
      <div className="flex flex-col items-end gap-0.5 self-start pt-1">
        <span className="text-[15px] font-semibold tracking-[-0.02em]">{timeParts(event.startsAt).clock}</span>
        <span className="text-[10px] font-semibold text-muted">{timeParts(event.startsAt).period}</span>
      </div>
    </article>
  );
}

function ContactLine({ value, verified }: { value: string; verified: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-2xl border border-line bg-surface px-4 py-3">
      <span className="truncate text-[15px]">{value}</span>
      <span
        className={`shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold ${verified ? "bg-lime text-ink" : "bg-line text-muted"}`}
      >
        {verified ? "Verified" : "Unverified"}
      </span>
    </div>
  );
}

/** Home: the screen you land on after signing in. */
export default function HomePage() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const account = useBottomSheet("account");

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

  async function handleSignOut() {
    await logout().catch(() => {
      /* the cookie is cleared server-side; move on regardless */
    });
    router.replace("/login");
  }

  if (!user) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  const firstName = user.name.trim().split(/\s+/)[0] || user.username;
  const avatarText = initials(user.name, user.username);
  const [nextEvent, ...moreEvents] = sampleEvents;

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
          <button type="button" onClick={account.open} aria-label="Your account" className="rounded-full">
            <Avatar text={avatarText} color={avatarColors.peach} size={44} />
          </button>
        </header>

        <div className="flex flex-col gap-8 px-5">
          <NextEventCard event={nextEvent} />

          <section className="flex flex-col gap-4">
            <SectionHeader eyebrow="Your momentum" title="Weekly activity" />
            <MomentumCard momentum={sampleMomentum} />
          </section>

          <section className="flex flex-col gap-4">
            <SectionHeader eyebrow="Near you" title="More events" />
            <div className="flex flex-col gap-3">
              {moreEvents.map((e) => (
                <EventRow key={e.id} event={e} />
              ))}
            </div>
          </section>
        </div>
      </div>

      <BottomNav active="home" onProfile={account.open} />

      <BottomSheet
        hash="account"
        title="Your account"
        footer={
          <Button variant="secondary" size="lg" fullWidth startIcon={<LogoutIcon />} onClick={handleSignOut}>
            Sign out
          </Button>
        }
      >
        <div className="flex flex-col gap-4 pb-2">
          <div className="flex items-center gap-3">
            <Avatar text={avatarText} color={avatarColors.peach} size={56} />
            <div className="flex flex-col">
              <span className="text-lg font-semibold tracking-[-0.02em]">{user.name}</span>
              <span className="text-sm text-muted">@{user.username}</span>
            </div>
          </div>
          {user.phone && <ContactLine value={user.phone} verified={user.phoneVerified} />}
          {user.email && <ContactLine value={user.email} verified={user.emailVerified} />}
          <p className="text-xs text-muted">
            You sign in with {user.signInMethod === "google" ? "Google" : "your phone number"}.
          </p>
        </div>
      </BottomSheet>
    </main>
  );
}
