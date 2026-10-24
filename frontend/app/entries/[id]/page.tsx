"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { formatList } from "@/components/events/EventBits";
import { CheckIcon, ClockIcon, LockIcon, SparkleIcon } from "@/components/icons";
import { ApiError, cancelEntry, me, myEntries, type Entry } from "@/lib/api";
import { formatWhen, rupees } from "@/lib/eventTime";
import { PAIRING_GAP, STARTING_RATING } from "@/lib/rating";
import { usePayEntry } from "@/lib/usePayEntry";
import { useSecondsLeft } from "@/lib/useSecondsLeft";

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

function Rings() {
  return (
    <div aria-hidden="true" className="relative size-[190px] shrink-0">
      <span className="absolute inset-0 rounded-full border border-dashed border-line" />
      <span className="absolute inset-[30px] rounded-full border border-line" />
      <span className="absolute top-[59px] left-[59px] flex size-[72px] items-center justify-center rounded-[22px] bg-lime text-ink shadow-[0_10px_30px_rgba(143,174,18,0.35)]">
        <SparkleIcon className="size-8" />
      </span>
    </div>
  );
}

function EntryStatus({
  entry,
  onChange,
  notice,
}: {
  entry: Entry;
  onChange: (e: Entry) => void;
  /** How checkout ended on the way here, if it didn't end in a payment. */
  notice: "failed" | "closed" | null;
}) {
  const payEntry = usePayEntry();
  const e = entry.event;
  const tz = e.venue.timezone;
  const secondsLeft = useSecondsLeft(entry.status === "pending_payment" ? entry.holdExpiresAt : null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const held = entry.status === "pending_payment" && secondsLeft > 0;
  const expired = entry.status === "pending_payment" && secondsLeft === 0;
  const paid = ["confirmed", "standby", "scheduled"].includes(entry.status);

  async function release() {
    setError(null);
    setBusy(true);
    try {
      onChange(await cancelEntry(entry.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  }

  let eyebrow = `${rupees(e.feePaise)} · ${e.title}`;
  let title: ReactNode;
  let body: string;
  if (paid) {
    eyebrow = `Paid ${eyebrow}`;
    title = (
      <>
        Finding your
        <br />
        <span className="bg-[linear-gradient(transparent_58%,#e2fb6c_58%)] px-0.5 dark:bg-none dark:text-lime">perfect rival.</span>
      </>
    );
    body = `We're pairing you with a player rated about ${STARTING_RATING - PAIRING_GAP}–${STARTING_RATING + PAIRING_GAP}. We'll notify you with your opponent, court and judge.`;
  } else if (held) {
    title = "Your spot is held.";
    body =
      notice === "failed"
        ? "The payment didn't go through, and you haven't been charged. Try again before the hold runs out."
        : "Pay to confirm your place in the pool. If you don't, the spot goes back when the hold runs out.";
  } else if (expired) {
    title = "Your hold ran out.";
    body = "The spot went back to the pool. You can try again while the event still has room.";
  } else {
    title = entry.status === "refunded" ? "Payment refunded." : "Entry cancelled.";
    body =
      entry.status === "refunded"
        ? "Your payment arrived after the spot had gone, so the full amount is on its way back to you."
        : "This entry no longer has a place in the pool.";
  }

  const minutes = Math.floor(secondsLeft / 60);
  const seconds = String(secondsLeft % 60).padStart(2, "0");

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col items-center px-[22px] pt-11 pb-48 text-center">
        <Rings />
        <p className="mt-6 text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">{eyebrow}</p>
        <h1 className="mt-2.5 text-[34px] leading-[1.04] font-bold tracking-[-0.04em]">{title}</h1>
        <p className="mt-2.5 max-w-[320px] text-[15px] leading-normal text-muted">{body}</p>

        {held && (
          <p className="mt-5 flex items-center gap-2 rounded-2xl bg-line/60 px-3.5 py-3 text-[13px] font-semibold">
            <ClockIcon className="size-4" />
            Held for{" "}
            <span className="tabular-nums">
              {minutes}:{seconds}
            </span>
          </p>
        )}

        <ol
          aria-label="What happens next"
          className="mt-5 flex w-full flex-col gap-3.5 rounded-[20px] border border-line bg-surface p-4 text-left"
        >
          <Step state="done" title="Spot held" detail={formatList(entry.formats)} />
          <Step state={paid ? "done" : held ? "now" : "todo"} title="Paid" />
          <Step state={paid ? "now" : "todo"} title="Finding your opponent" detail={paid ? "Usually well before match day" : undefined} />
          <Step state="todo" title="Court and judge assigned" />
          <Step state="todo" title={`Match day · ${formatWhen(e.startsAt, tz)}`} />
        </ol>
        {(error ?? payEntry.error) && (
          <p role="alert" className="mt-3 w-full rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
            {error ?? payEntry.error}
          </p>
        )}
      </div>

      <footer className="fixed bottom-0 left-1/2 z-10 flex w-full max-w-[480px] -translate-x-1/2 flex-col gap-2.5 bg-background px-5 pt-3 pb-[max(28px,env(safe-area-inset-bottom))]">
        {held && (
          <button
            type="button"
            onClick={async () => {
              const outcome = await payEntry.pay(entry.id);
              if ("paid" in outcome) onChange(outcome.paid);
            }}
            disabled={payEntry.busy}
            className="flex min-h-[54px] items-center justify-center gap-2.5 rounded-2xl bg-primary text-base font-semibold text-on-primary disabled:opacity-50"
          >
            <LockIcon className="size-4" />
            {payEntry.busy ? "Paying…" : `Pay ${rupees(e.feePaise)}`}
          </button>
        )}
        {expired && (
          <Link
            href={`/events/${e.id}/join`}
            className="flex min-h-[54px] items-center justify-center rounded-2xl bg-primary text-base font-semibold text-on-primary"
          >
            Try again
          </Link>
        )}
        <Link
          href="/dashboard"
          className="flex min-h-[54px] items-center justify-center rounded-2xl border border-line bg-surface text-base font-semibold"
        >
          Back to home
        </Link>
        {(held || expired) && (
          <button
            type="button"
            onClick={() => void release()}
            disabled={busy}
            className="min-h-11 text-sm font-semibold text-muted underline underline-offset-4 disabled:opacity-50"
          >
            {held ? "Release this spot" : "Remove this entry"}
          </button>
        )}
      </footer>
    </main>
  );
}

/** One entry: held while paying, then waiting to be paired. */
export default function EntryPage() {
  const { id } = useParams<{ id: string }>();
  const payment = useSearchParams().get("payment");
  const router = useRouter();
  const [entry, setEntry] = useState<Entry | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    me()
      .then(() => myEntries())
      .then((entries) => {
        const found = entries.find((x) => x.id === id);
        if (found) setEntry(found);
        else setError("This entry isn't yours, or doesn't exist.");
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Couldn't load the entry.");
      });
  }, [id, router]);

  if (!entry) {
    return (
      <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background px-6 text-center">
        <p role={error ? "alert" : undefined} className="text-sm text-muted">
          {error ?? "Loading…"}
        </p>
        {error && (
          <Link href="/dashboard" className="text-sm font-semibold underline underline-offset-4">
            Back to Home
          </Link>
        )}
      </main>
    );
  }
  return (
    <EntryStatus entry={entry} onChange={setEntry} notice={payment === "failed" || payment === "closed" ? payment : null} />
  );
}
