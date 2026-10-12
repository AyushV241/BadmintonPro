"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { BottomNav } from "@/components/app/BottomNav";
import { EventRow, NextEventCard, joinable } from "@/components/events/EventBits";
import { Switch } from "@/components/form/Switch";
import { ArrowRightIcon, LogoutIcon, ShieldIcon } from "@/components/icons";
import { BottomSheet, Button, useBottomSheet } from "@/components/ui";
import { setAdminMode, useAdminMode } from "@/lib/adminMode";
import { logout, me, type User } from "@/lib/api";
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

function NearbyEvents() {
  const nearby = useNearbyEvents();

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
      <NextEventCard event={next} />
      <RatingStrip />
      {more.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="text-lg font-semibold tracking-[-0.02em]">
            {nearby.byDistance ? "Events near you" : "Events in Bangalore"}
          </h2>
          <div className="flex flex-col gap-3">
            {more.map((e) => (
              <EventRow key={e.id} event={e} />
            ))}
          </div>
        </section>
      )}
    </>
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
          <p className="pt-2 text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">Settings</p>
          {/* Open to everyone for now; later only people with the admin role see it. */}
          <Switch
            checked={adminMode}
            onChange={setAdminMode}
            label="Admin mode"
            description="Create venues and events, and assign judges."
          />
        </div>
      </BottomSheet>
    </main>
  );
}
