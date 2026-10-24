"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { directionsUrl } from "@/components/events/VenueMap";
import { CalendarIcon, CheckIcon, ClockIcon, LockIcon, PinIcon, XIcon } from "@/components/icons";
import {
  ApiError,
  acceptChallenge,
  declineChallenge,
  getChallenge,
  me,
  withdrawChallenge,
  type Challenge,
  type Player,
} from "@/lib/api";
import { clockTime, formatWhen, rupees } from "@/lib/eventTime";
import { initials } from "@/lib/initials";
import { usePayEntry } from "@/lib/usePayEntry";
import { formatCountdown, useSecondsLeft } from "@/lib/useSecondsLeft";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");
const firstName = (p: Player) => p.name.split(" ")[0] || p.username;

function VersusCard({ c }: { c: Challenge }) {
  const me = c.role === "challenger" ? c.challenger : c.opponent;
  const them = c.role === "challenger" ? c.opponent : c.challenger;
  const side = (p: Player, mine: boolean) => (
    <div className="flex min-w-0 flex-col items-center gap-1.5 text-center">
      <Avatar text={initials(p.name, p.username)} color={mine ? avatarColors.lime : avatarColors.blue} size={58} />
      <span className="max-w-full truncate text-[15px] font-semibold">{mine ? "You" : p.name}</span>
      <span className="text-xs text-[#a3a596]">Rating {p.rating}</span>
    </div>
  );
  return (
    <section
      aria-label="The match"
      className="flex flex-col gap-4 rounded-3xl bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0] dark:border dark:border-[#30342a] dark:bg-[#1c1f18]"
    >
      <div className="flex items-center justify-between">
        <span className="rounded-full bg-white/10 px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-lime">
          SINGLES · RATED
        </span>
        <span className="text-[11px] font-semibold tracking-[0.12em] text-[#a3a596] uppercase">
          {formatWhen(c.event.startsAt, c.event.venue.timezone).split(" · ")[0]}
        </span>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
        {side(them, false)}
        <span className="text-[22px] font-semibold tracking-[-0.02em] text-[#a3a596]">VS</span>
        {side(me, true)}
      </div>
    </section>
  );
}

function Row({ icon, title, detail, action }: { icon: ReactNode; title: string; detail: string; action?: ReactNode }) {
  return (
    <div className="flex items-center gap-3 border-b border-line px-3.5 py-3 last:border-b-0">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-background">{icon}</span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[15px] font-semibold">{title}</span>
        <span className="text-[13px] text-muted">{detail}</span>
      </span>
      {action}
    </div>
  );
}

function Details({ c }: { c: Challenge }) {
  const tz = c.event.venue.timezone;
  const directions = directionsUrl(c.event.venue);
  const m = c.match;
  return (
    <div className="overflow-hidden rounded-[20px] border border-line bg-surface">
      <Row
        icon={<CalendarIcon className="size-[19px]" />}
        title={m ? `${formatWhen(m.startsAt, tz)} · ${m.court.label}` : `${formatWhen(c.preferredStart, tz).split(" · ")[0]} · around ${clockTime(c.preferredStart, tz)}`}
        detail={m ? (m.judge ? `Judge ${m.judge.name}` : "Judge to be assigned") : c.event.title}
      />
      <Row
        icon={<PinIcon className="size-[19px]" />}
        title={c.event.venue.name}
        detail={c.event.venue.address || c.event.venue.city}
        action={
          directions && (
            <a href={directions} target="_blank" rel="noreferrer" className="flex min-h-11 items-center text-[13px] font-semibold">
              Directions
            </a>
          )
        }
      />
      {!m && (
        <Row
          icon={<LockIcon className="size-[19px]" />}
          title={`${rupees(c.event.feePaise)} each`}
          detail="Paid after accepting. Refunded automatically if the match isn't fixed."
        />
      )}
    </div>
  );
}

function Step({ state, title, detail }: { state: "done" | "now" | "todo"; title: string; detail?: string }) {
  return (
    <li className="flex items-start gap-3">
      {state === "done" ? (
        <span className="flex size-[26px] shrink-0 items-center justify-center rounded-full bg-lime text-ink">
          <CheckIcon className="size-3.5" strokeWidth={3} />
        </span>
      ) : state === "now" ? (
        <span className="flex size-[26px] shrink-0 items-center justify-center rounded-full border-2 border-foreground">
          <span className="size-2.5 rounded-full bg-foreground" />
        </span>
      ) : (
        <span className="size-[26px] shrink-0 rounded-full border-2 border-line" />
      )}
      <span className="flex flex-col gap-0.5 pt-[3px]">
        <span className={`text-[15px] ${state === "todo" ? "font-medium text-muted" : "font-semibold"}`}>{title}</span>
        {detail && <span className="text-[13px] text-muted">{detail}</span>}
      </span>
    </li>
  );
}

const ended: Partial<Record<Challenge["status"], string>> = {
  declined: "Challenge declined.",
  withdrawn: "Challenge withdrawn.",
  expired: "Challenge expired.",
  failed: "No slot was left.",
};

function ChallengeView({ challenge: c, onChange }: { challenge: Challenge; onChange: (c: Challenge) => void }) {
  const payEntry = usePayEntry();
  const secondsLeft = useSecondsLeft(c.status === "pending" || c.status === "accepted" ? c.expiresAt : null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const them = c.role === "challenger" ? c.opponent : c.challenger;
  const myPaid = c.myEntry ? ["confirmed", "scheduled"].includes(c.myEntry.status) : false;
  const open = c.status === "pending" || c.status === "accepted";

  async function act(kind: "accept" | "decline" | "withdraw") {
    if (kind !== "accept" && !window.confirm(kind === "decline" ? "Decline this challenge?" : "Withdraw this challenge?")) return;
    setError(null);
    setBusy(kind);
    try {
      onChange(await (kind === "accept" ? acceptChallenge(c.id) : kind === "decline" ? declineChallenge(c.id) : withdrawChallenge(c.id)));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(null);
    }
  }

  async function pay() {
    if (!c.myEntry) return;
    const outcome = await payEntry.pay(c.myEntry.id);
    if ("paid" in outcome) onChange(await getChallenge(c.id).catch(() => c));
  }

  let eyebrow: string;
  let title: string;
  if (c.status === "scheduled") {
    eyebrow = "Match fixed";
    title = `You vs ${firstName(them)}.`;
  } else if (c.status === "accepted") {
    eyebrow = "Challenge accepted";
    title = myPaid ? `Waiting for ${firstName(them)} to pay.` : "Pay to lock it in.";
  } else if (c.status === "pending") {
    eyebrow = c.role === "opponent" ? "Challenge received" : "Challenge sent";
    title = c.role === "opponent" ? `${firstName(them)} wants to play you.` : `Waiting for ${firstName(them)}.`;
  } else {
    eyebrow = "Challenge";
    title = ended[c.status] ?? "Challenge closed.";
  }

  const endedNote =
    c.status === "expired"
      ? "The 4 hours ran out before you'd both paid. Any payment made is on its way back."
      : c.status === "failed"
        ? "You both paid, but the event had no free slot left. Both payments are on their way back."
        : c.status === "declined" || c.status === "withdrawn"
          ? "No match this time. Any payment made is on its way back."
          : null;

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col gap-3.5 px-5 pt-3.5 pb-48">
        <header className="flex items-center justify-between">
          <Link href="/matches" aria-label="Close" className="-ml-2 flex size-11 items-center justify-center">
            <XIcon className="size-[22px]" />
          </Link>
          {open && (
            <span className="flex items-center gap-1.5 rounded-full bg-line/60 px-3 py-1.5 text-xs font-semibold">
              <ClockIcon className="size-3.5" />
              {secondsLeft > 0 ? `${formatCountdown(secondsLeft)} left` : "Time's up"}
            </span>
          )}
        </header>
        <div className="flex flex-col gap-1.5">
          <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">{eyebrow}</p>
          <h1 className="text-[32px] leading-[1.04] font-bold tracking-[-0.04em]">{title}</h1>
        </div>

        <VersusCard c={c} />
        <Details c={c} />

        {c.status === "accepted" && (
          <ol aria-label="Progress" className="flex flex-col gap-3.5 rounded-[20px] border border-line bg-surface p-4">
            <Step state="done" title={`${c.role === "challenger" ? firstName(them) : "You"} accepted`} />
            <Step state={myPaid ? "done" : "now"} title={`You pay ${rupees(c.event.feePaise)}`} />
            <Step state={c.theirEntryPaid ? "done" : "todo"} title={`${firstName(them)} pays ${rupees(c.event.feePaise)}`} />
            <Step state="todo" title="Slot fixed: court, time and judge" />
          </ol>
        )}
        {endedNote && <p className="rounded-2xl bg-line/60 px-3.5 py-3 text-[13px] leading-snug">{endedNote}</p>}
        {(error ?? payEntry.error) && (
          <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error ?? payEntry.error}
          </p>
        )}
      </div>

      <footer className="fixed bottom-0 left-1/2 z-10 flex w-full max-w-[480px] -translate-x-1/2 flex-col gap-2.5 border-t border-line bg-surface px-5 pt-3 pb-[max(28px,env(safe-area-inset-bottom))]">
        {c.status === "pending" && c.role === "opponent" && (
          <div className="flex gap-2.5">
            <button
              type="button"
              onClick={() => void act("decline")}
              disabled={!!busy}
              className="min-h-[54px] flex-1 rounded-2xl border border-line bg-surface text-base font-semibold disabled:opacity-50"
            >
              Decline
            </button>
            <button
              type="button"
              onClick={() => void act("accept")}
              disabled={!!busy || secondsLeft === 0}
              className="min-h-[54px] flex-[1.6] rounded-2xl bg-primary text-base font-semibold text-on-primary disabled:opacity-50"
            >
              {busy === "accept" ? "Accepting…" : "Accept"}
            </button>
          </div>
        )}
        {c.status === "accepted" && !myPaid && c.myEntry && (
          <button
            type="button"
            onClick={() => void pay()}
            disabled={payEntry.busy || secondsLeft === 0}
            className="flex min-h-[54px] items-center justify-center gap-2.5 rounded-2xl bg-primary text-base font-semibold text-on-primary disabled:opacity-50"
          >
            <LockIcon className="size-4" />
            {payEntry.busy ? "Paying…" : `Pay ${rupees(c.event.feePaise)}`}
          </button>
        )}
        {c.status === "scheduled" && (
          <Link
            href="/matches"
            className="flex min-h-[54px] items-center justify-center rounded-2xl bg-primary text-base font-semibold text-on-primary"
          >
            View my matches
          </Link>
        )}
        {!(c.status === "pending" && c.role === "opponent") && c.status !== "scheduled" && (
          <Link
            href="/matches"
            className="flex min-h-[54px] items-center justify-center rounded-2xl border border-line bg-surface text-base font-semibold"
          >
            Back to my matches
          </Link>
        )}
        {open && c.role === "challenger" && (
          <button
            type="button"
            onClick={() => void act("withdraw")}
            disabled={!!busy}
            className="min-h-11 text-sm font-semibold text-muted underline underline-offset-4 disabled:opacity-50"
          >
            Withdraw challenge
          </button>
        )}
      </footer>
    </main>
  );
}

/** One challenge, from either side: answer it, pay for it, or see how it ended. */
export default function ChallengePage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    me()
      .then(() => getChallenge(id))
      .then(setChallenge)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError && err.status === 404 ? "This challenge isn't yours, or doesn't exist." : message(err));
      });
  }, [id, router]);

  if (!challenge) {
    return (
      <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background px-6 text-center">
        <p role={error ? "alert" : undefined} className="text-sm text-muted">
          {error ?? "Loading…"}
        </p>
        {error && (
          <Link href="/matches" className="text-sm font-semibold underline underline-offset-4">
            Back to my matches
          </Link>
        )}
      </main>
    );
  }
  return <ChallengeView challenge={challenge} onChange={setChallenge} />;
}
