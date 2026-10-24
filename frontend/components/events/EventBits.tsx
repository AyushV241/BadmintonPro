import Image from "next/image";
import Link from "next/link";
import { ArrowRightIcon, ClockIcon, PinIcon } from "@/components/icons";
import type { BadmintonEvent, Entry, EntryStatus, EventFormat, MyMatch } from "@/lib/api";
import { clockTime, dateTile, rupees } from "@/lib/eventTime";
import courtPhoto from "@/public/images/court-night.jpg";

const formatNames: Record<EventFormat, string> = { singles: "Singles", doubles: "Doubles", mixed: "Mixed" };

/** "Singles · Doubles" */
export function formatList(formats: EventFormat[]) {
  return formats.map((f) => formatNames[f]).join(" · ");
}

export function formatName(format: EventFormat) {
  return formatNames[format];
}

/** The last word of the title in lime, as in the designs. */
export function AccentTitle({ title }: { title: string }) {
  const words = title.split(" ");
  const last = words.pop();
  return (
    <>
      {words.join(" ")} <span className="text-lime">{last}</span>
    </>
  );
}

/** Where the signed-in player stands in an event, when they've joined it. */
export type MyEventStatus = {
  /** "Finding your perfect match", "Match at 8:30 PM", "Pay to confirm"… */
  label: string;
  /** The same, short enough for the photo card's button. */
  short: string;
  /** The button: "View entry", "View match", "Pay to confirm". */
  cta: string;
  href: string;
  /** "action": something to do (pay); "in": nothing to do but wait or play. */
  tone: "in" | "action";
  /** Entries still counting towards the 3-per-event limit. */
  live: number;
};

const LIVE: EntryStatus[] = ["confirmed", "standby", "scheduled"];

/**
 * The player's standing in an event from their entries (and matches, for
 * the time). A scheduled match beats a paid entry, which beats a held one.
 * Null when they have nothing live there.
 */
export function myStatusIn(eventId: string, entries: Entry[], matches: MyMatch[] = []): MyEventStatus | null {
  const mine = entries.filter((e) => e.event.id === eventId);
  const isLive = (e: Entry) => LIVE.includes(e.status) || (e.status === "pending_payment" && !e.holdExpired);
  const live = mine.filter(isLive).length;
  if (live === 0) return null;

  const scheduled = mine.find((e) => e.status === "scheduled");
  if (scheduled) {
    const match = matches.find((m) => m.event?.id === eventId && ["scheduled", "ready", "live"].includes(m.status));
    const label = match ? `Match at ${clockTime(match.startsAt, match.venue.timezone)}` : "Match scheduled";
    return { label, short: label, cta: "View match", href: "/matches", tone: "in", live };
  }
  const paid = mine.find((e) => e.status === "confirmed");
  if (paid) {
    return { label: "Finding your perfect match", short: "Finding your match", cta: "View entry", href: `/entries/${paid.id}`, tone: "in", live };
  }
  const standby = mine.find((e) => e.status === "standby");
  if (standby) return { label: "On standby", short: "On standby", cta: "View entry", href: `/entries/${standby.id}`, tone: "in", live };
  const held = mine.find((e) => e.status === "pending_payment" && !e.holdExpired)!;
  return { label: "Pay to confirm", short: "Pay to confirm", cta: "Pay to confirm", href: `/entries/${held.id}`, tone: "action", live };
}

/** Whether a player can still join: before the cutoff, with room left. */
export function joinable(e: BadmintonEvent, now = Date.now()) {
  return e.status === "published" && now < Date.parse(e.registrationClosesAt) && e.spotsLeft > 0;
}

export function spotsLabel(e: BadmintonEvent) {
  return e.spotsLeft === 1 ? "1 spot left" : `${e.spotsLeft} spots left`;
}

/** The court photo behind event headers. */
export function CourtPhoto({ className = "" }: { className?: string }) {
  return (
    <>
      <Image
        src={courtPhoto}
        alt=""
        fill
        placeholder="blur"
        sizes="(max-width: 480px) 100vw, 480px"
        className={`object-cover object-[60%_30%] ${className}`}
      />
      <div
        aria-hidden="true"
        className="absolute inset-0 bg-[linear-gradient(90deg,rgba(21,23,18,0.85)_0%,rgba(21,23,18,0.4)_60%,rgba(21,23,18,0.15)_100%),linear-gradient(180deg,rgba(21,23,18,0)_45%,rgba(21,23,18,0.6)_100%)]"
      />
    </>
  );
}

/** The photo card for the next event near you. */
export function NextEventCard({ event, mine }: { event: BadmintonEvent; mine?: MyEventStatus | null }) {
  const tz = event.venue.timezone;
  return (
    <Link
      href={mine ? mine.href : `/events/${event.id}`}
      aria-label={mine ? `${event.title}: ${mine.label}` : `${event.title}, view and join`}
      className="relative block h-[230px] overflow-hidden rounded-[26px] bg-ink text-white"
    >
      <CourtPhoto />
      <div className="absolute inset-0 flex flex-col justify-between p-[18px]">
        <span className="flex items-center gap-2 self-start rounded-full bg-ink/55 px-2.5 py-1.5 text-[11px] font-semibold tracking-[0.12em] uppercase">
          <span className="size-[7px] rounded-full bg-lime shadow-[0_0_0_3px_rgba(226,251,108,0.25)]" />
          {mine ? (mine.tone === "action" ? "Spot held" : "You're in") : `Next near you · ${spotsLabel(event)}`}
        </span>
        <div className="flex flex-col gap-1.5">
          <p className="text-[11px] font-semibold tracking-[0.14em] text-lime uppercase">{formatList(event.formats)}</p>
          <h3 className="text-[28px] leading-[1.02] font-bold tracking-[-0.045em]">
            <AccentTitle title={event.title} />
          </h3>
          <div className="mt-1 flex items-center justify-between gap-2">
            <span className="flex items-center gap-3 text-[13px] text-white/80">
              <span className="flex items-center gap-1.5">
                <ClockIcon className="size-3.5" />
                {dateTile(event.startsAt, tz).weekday} · {clockTime(event.startsAt, tz)}
              </span>
              {event.distanceKm !== undefined && (
                <span className="flex items-center gap-1.5">
                  <PinIcon className="size-3.5" />
                  {event.distanceKm.toFixed(1)} km
                </span>
              )}
            </span>
            <span className="flex min-h-11 shrink-0 items-center gap-1.5 rounded-full bg-lime px-4 text-sm font-semibold text-ink">
              {mine ? mine.short : `Join · ${rupees(event.feePaise)}`}
              <ArrowRightIcon className="size-4" />
            </span>
          </div>
        </div>
      </div>
    </Link>
  );
}

/** A day tile: "SAT / 10". */
export function DateTile({ iso, timeZone }: { iso: string; timeZone: string }) {
  const { weekday, day } = dateTile(iso, timeZone);
  return (
    <span className="flex h-14 w-[52px] shrink-0 flex-col items-center justify-center gap-px rounded-[14px] bg-background">
      <span className="text-[10px] font-semibold tracking-[0.12em] text-muted uppercase">{weekday}</span>
      <span className="text-xl leading-none font-semibold tracking-[-0.03em]">{day}</span>
    </span>
  );
}

export function EventRow({ event, mine }: { event: BadmintonEvent; mine?: MyEventStatus | null }) {
  const tz = event.venue.timezone;
  return (
    <Link
      href={`/events/${event.id}`}
      className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3"
    >
      <DateTile iso={event.startsAt} timeZone={tz} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <h3 className="truncate text-[15px] font-semibold tracking-[-0.01em]">{event.title}</h3>
        <p className="flex items-center gap-1 text-xs text-muted">
          <PinIcon className="size-3 shrink-0" />
          <span className="truncate">{event.venue.name}</span>
          {event.distanceKm !== undefined && <span className="shrink-0">· {event.distanceKm.toFixed(1)} km</span>}
        </p>
        <p className={`text-xs font-semibold ${mine || joinable(event) ? "text-lime-text" : "text-muted"}`}>
          {formatList(event.formats)} ·{" "}
          {mine ? mine.label : joinable(event) ? spotsLabel(event) : event.spotsLeft > 0 ? "Joining closed" : "Full"}
        </p>
      </div>
      <span className="self-start pt-1 text-[13px] font-semibold">{clockTime(event.startsAt, tz)}</span>
    </Link>
  );
}
