"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { AccentTitle, CourtPhoto, formatList, spotsLabel } from "@/components/events/EventBits";
import { VenueMap } from "@/components/events/VenueMap";
import { ArrowRightIcon, CalendarIcon, ChevronLeftIcon, PinIcon, ShieldCheckIcon, TargetIcon } from "@/components/icons";
import { ApiError, getEvent, type BadmintonEvent } from "@/lib/api";
import { clockTime, formatWindow, rupees } from "@/lib/eventTime";

function InfoRow({ icon, title, detail, action }: { icon: ReactNode; title: string; detail: string; action?: ReactNode }) {
  return (
    <div className="flex items-center gap-3 border-b border-line px-3.5 py-3 last:border-b-0">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-background text-foreground">
        {icon}
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-[15px] font-semibold">{title}</span>
        <span className="text-[13px] text-muted">{detail}</span>
      </span>
      {action}
    </div>
  );
}

function bandText(e: BadmintonEvent) {
  if (e.ratingMin === null && e.ratingMax === null) return "Open to every rating";
  if (e.ratingMin === null) return `Rating band up to ${e.ratingMax}`;
  if (e.ratingMax === null) return `Rating band ${e.ratingMin} and up`;
  return `Rating band ${e.ratingMin}–${e.ratingMax}`;
}

/** Why the Join button is off, or null when joining is open. */
function closedReason(e: BadmintonEvent, now: number): string | null {
  if (e.status === "cancelled") return "Event cancelled";
  if (e.status !== "published") return "Not open";
  if (now >= Date.parse(e.registrationClosesAt)) return "Joining closed";
  if (e.spotsLeft <= 0) return "Event full";
  return null;
}

function EventDetails({ event: e }: { event: BadmintonEvent }) {
  const tz = e.venue.timezone;
  const taken = e.capacity - e.spotsLeft;
  const courts = e.courts?.length ?? 0;
  // Read once on mount: whether joining is still open when the page opened.
  const [now] = useState(() => Date.now());
  const closed = closedReason(e, now);

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto w-full max-w-[480px] pb-36">
        <div className="relative h-[300px] overflow-hidden bg-ink text-white">
          <CourtPhoto />
          <div className="absolute inset-x-0 top-0 flex justify-between p-4">
            <Link
              href="/dashboard"
              aria-label="Back"
              className="flex size-11 items-center justify-center rounded-[14px] border border-white/20 bg-white/15 backdrop-blur"
            >
              <ChevronLeftIcon className="size-[22px]" />
            </Link>
          </div>
          <div className="absolute inset-x-0 bottom-0 flex flex-col gap-2.5 px-[22px] pb-9">
            <div className="flex flex-wrap gap-2">
              <span className="rounded-full bg-ink/55 px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] uppercase">
                {formatList(e.formats)}
              </span>
              <span className="rounded-full bg-lime px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] text-ink uppercase">
                {spotsLabel(e)}
              </span>
            </div>
            <h1 className="text-[32px] leading-[1.02] font-bold tracking-[-0.04em]">
              <AccentTitle title={e.title} />
            </h1>
          </div>
        </div>

        <div className="relative -mt-6 flex flex-col gap-3.5 rounded-t-[26px] bg-background px-5 pt-5">
          <div className="overflow-hidden rounded-[20px] border border-line bg-surface">
            <InfoRow
              icon={<CalendarIcon className="size-[19px]" />}
              title={formatWindow(e.startsAt, e.endsAt, tz)}
              detail={`Joining closes at ${clockTime(e.registrationClosesAt, tz)}`}
            />
            <InfoRow
              icon={<PinIcon className="size-[19px]" />}
              title={e.venue.name}
              detail={courts ? `${courts} ${courts === 1 ? "court" : "courts"} booked` : e.venue.address}
            />
            <InfoRow
              icon={<TargetIcon className="size-[19px]" />}
              title="Best of 3 · games to 21"
              detail={bandText(e)}
            />
            <InfoRow
              icon={<ShieldCheckIcon className="size-[19px]" />}
              title="A judge on every match"
              detail="Live scores in the app"
            />
          </div>

          <VenueMap venue={e.venue} />

          <section aria-label="Entries" className="flex flex-col gap-3 rounded-[20px] border border-line bg-surface px-4 py-3.5">
            <div className="flex items-center justify-between">
              <span className="text-[15px] font-semibold">Entries</span>
              <span className="text-[13px] text-muted">
                {taken} of {e.capacity} taken
              </span>
            </div>
            <div aria-hidden="true" className="h-1.5 rounded-full bg-line">
              <div
                className="h-full rounded-full bg-lime"
                style={{ width: `${e.capacity ? Math.min(100, (taken / e.capacity) * 100) : 0}%` }}
              />
            </div>
            <p className="text-xs text-muted">One entry is one match. You can hold up to 3 here.</p>
          </section>
        </div>
      </div>

      <footer className="fixed bottom-0 left-1/2 z-10 flex w-full max-w-[480px] -translate-x-1/2 items-center justify-between gap-4 border-t border-line bg-surface px-5 pt-3.5 pb-[max(28px,env(safe-area-inset-bottom))]">
        <div className="flex flex-col">
          <span className="text-[26px] leading-tight font-semibold tracking-[-0.04em]">{rupees(e.feePaise)}</span>
          <span className="text-xs text-muted">per entry · one match</span>
        </div>
        {/* TODO(step 7): a Challenge button next to Join, as in the design. */}
        {closed ? (
          <span className="flex min-h-[54px] items-center rounded-2xl bg-line px-6 text-base font-semibold text-muted">
            {closed}
          </span>
        ) : (
          <Link
            href={`/events/${e.id}/join`}
            className="flex min-h-[54px] items-center gap-2.5 rounded-2xl bg-primary px-6 text-base font-semibold text-on-primary"
          >
            Join
            <ArrowRightIcon className="size-[18px]" />
          </Link>
        )}
      </footer>
    </main>
  );
}

/** Event details. Public: anyone with the link can see a published event. */
export default function EventPage() {
  const { id } = useParams<{ id: string }>();
  const [event, setEvent] = useState<BadmintonEvent | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getEvent(id)
      .then(setEvent)
      .catch((err) =>
        setError(err instanceof ApiError && err.status === 404 ? "This event doesn't exist." : "Couldn't load the event."),
      );
  }, [id]);

  if (!event) {
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
  return <EventDetails event={event} />;
}
