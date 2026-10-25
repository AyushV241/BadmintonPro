"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { DateTile, formatName } from "@/components/events/EventBits";
import { PlayerSearch } from "@/components/events/PlayerSearch";
import { FieldError } from "@/components/form/FieldError";
import { CheckIcon, ChevronLeftIcon, ClockIcon, LockIcon, ShieldCheckIcon, TicketIcon, UserIcon, XIcon } from "@/components/icons";
import { BottomSheet, useBottomSheet } from "@/components/ui";
import { ApiError, createEntry, getEvent, me, type BadmintonEvent, type EventFormat, type PublicUser } from "@/lib/api";
import { formatWindow, rupees } from "@/lib/eventTime";
import { initials } from "@/lib/initials";
import { useFormErrors } from "@/lib/useFormErrors";
import { usePayEntry } from "@/lib/usePayEntry";
import { PAIRING_GAP, STARTING_RATING } from "@/lib/rating";

const ALL_FORMATS: EventFormat[] = ["singles", "doubles", "mixed"];
const isTeam = (f: EventFormat) => f !== "singles";

function FormatChip({
  format,
  pressed,
  offered,
  onToggle,
}: {
  format: EventFormat;
  pressed: boolean;
  offered: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      disabled={!offered}
      onClick={onToggle}
      className={`flex min-h-11 items-center justify-center gap-1.5 rounded-xl text-[15px] font-semibold transition-colors disabled:opacity-40 ${
        pressed ? "bg-lime text-ink" : "bg-white/10 text-white"
      }`}
    >
      {pressed && <CheckIcon className="size-3.5" strokeWidth={3} />}
      {formatName(format)}
    </button>
  );
}

function JoinForm({ event: e }: { event: BadmintonEvent }) {
  const router = useRouter();
  const partnerSheet = useBottomSheet("partner");
  const [formats, setFormats] = useState<EventFormat[]>(() => (e.formats.includes("singles") ? ["singles"] : [e.formats[0]]));
  const [partner, setPartner] = useState<PublicUser | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const payEntry = usePayEntry();
  const { errors, field, show, clear } = useFormErrors<"formats" | "partner">();
  const tz = e.venue.timezone;
  const notOffered = ALL_FORMATS.filter((f) => !e.formats.includes(f));
  const team = formats.some(isTeam);

  function toggle(f: EventFormat) {
    // Built on the latest formats, so two quick taps both count.
    setFormats((current) => (current.includes(f) ? current.filter((x) => x !== f) : [...current, f]));
    clear("formats");
  }

  // Doubles and mixed gone: no partner to keep.
  if (!team && partner) {
    setPartner(null);
    clear("partner");
  }

  async function pay() {
    setError(null);
    if (show({ formats: formats.length === 0 ? "Pick at least one format." : undefined })) return;
    setBusy(true);
    let entryId: string;
    try {
      entryId = (await createEntry(e.id, formats, team && partner ? partner.username : undefined)).id;
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Something went wrong.";
      // A partner problem belongs on the partner row; the rest (full, closed,
      // rating band) is about the event as a whole.
      if (err instanceof ApiError && (msg.includes("partner") || msg.includes("@"))) show({ partner: msg });
      else setError(msg);
      setBusy(false);
      return;
    }
    // The spot is held now; whatever happens at checkout, the entry page
    // shows where things stand and can retry the payment.
    const outcome = await payEntry.pay(entryId);
    router.replace(
      "paid" in outcome ? `/entries/${entryId}` : `/entries/${entryId}?payment=${"failed" in outcome ? "failed" : "closed"}`,
    );
  }

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col pb-36">
        <header className="flex items-center gap-1.5 px-3 pt-3.5 pb-1.5">
          <Link href={`/events/${e.id}`} aria-label="Back" className="flex size-11 items-center justify-center">
            <ChevronLeftIcon className="size-[22px]" />
          </Link>
          <div className="flex flex-col gap-0.5">
            <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">Find a match</p>
            <h1 className="text-2xl font-semibold tracking-[-0.04em]">Join &amp; pay</h1>
          </div>
        </header>

        <div className="flex flex-col gap-3 px-5 pt-2">
          <div className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-2.5 pr-1.5">
            <DateTile iso={e.startsAt} timeZone={tz} />
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="truncate text-[15px] font-semibold">{e.title}</span>
              <span className="truncate text-xs text-muted">
                {formatWindow(e.startsAt, e.endsAt, tz).split(" · ")[1]} · {e.venue.name}
              </span>
            </span>
            <Link href="/dashboard" className="flex min-h-11 items-center px-2.5 text-[13px] font-semibold underline underline-offset-[3px]">
              Change
            </Link>
          </div>

          <section
            aria-label="Your entry"
            className="flex flex-col gap-3.5 rounded-3xl bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0] border border-transparent dark:border-[#30342a] dark:bg-[#1c1f18]"
          >
            <div className="flex flex-col gap-1">
              <p className="text-[11px] font-semibold tracking-[0.14em] text-[#a3a596] uppercase">Formats you&rsquo;ll play</p>
              <p className="text-[13px] text-[#a3a596]">Pick one or more. Your entry gets one match, in one of them.</p>
            </div>
            <div {...field("formats")} className="flex flex-col gap-2">
            <div
              role="group"
              aria-label="Formats"
              aria-describedby={errors.formats ? "join-formats-error" : undefined}
              className="grid grid-cols-3 gap-2"
            >
              {ALL_FORMATS.map((f) => (
                <FormatChip
                  key={f}
                  format={f}
                  pressed={formats.includes(f)}
                  offered={e.formats.includes(f)}
                  onToggle={() => toggle(f)}
                />
              ))}
            </div>
            <FieldError id="join-formats-error" message={errors.formats} className="text-[#ff9b8c]" />
            </div>
            {notOffered.length > 0 && (
              <p className="-mt-1.5 text-xs text-[#a3a596]">
                {notOffered.map(formatName).join(" and ")} {notOffered.length === 1 ? "isn't" : "aren't"} offered at this event.
              </p>
            )}

            {team && (
              <div {...field("partner")} className="flex flex-col gap-2">
              <div className="flex items-center gap-3 rounded-2xl bg-white/10 px-3 py-2.5">
                {partner ? (
                  <Avatar text={initials(partner.name, partner.username)} color={avatarColors.blue} size={36} />
                ) : (
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-full border-2 border-dashed border-[#a3a596] text-[#a3a596]">
                    <UserIcon className="size-4" />
                  </span>
                )}
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-sm font-semibold">{partner ? partner.name : "Doubles partner"}</span>
                  <span className="truncate text-xs text-[#a3a596]">
                    {partner ? `@${partner.username}` : "We'll pick one if you don't"}
                  </span>
                </span>
                {partner ? (
                  <button
                    type="button"
                    onClick={() => setPartner(null)}
                    aria-label="Remove partner"
                    className="flex size-11 items-center justify-center rounded-xl text-[#a3a596]"
                  >
                    <XIcon className="size-5" />
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={partnerSheet.open}
                    className="min-h-11 rounded-xl bg-lime px-3 text-[13px] font-semibold text-ink"
                  >
                    Choose
                  </button>
                )}
              </div>
              <FieldError id="join-partner-error" message={errors.partner} className="text-[#ff9b8c]" />
              </div>
            )}

            <div className="flex items-center justify-between border-t border-[#33372c] pt-3.5">
              <span className="flex flex-col gap-0.5">
                <span className="text-[15px] font-semibold">Your rating</span>
                <span className="text-xs text-[#a3a596]">
                  Paired within about {PAIRING_GAP}: {STARTING_RATING - PAIRING_GAP}–{STARTING_RATING + PAIRING_GAP}
                </span>
              </span>
              <span className="text-2xl font-semibold tracking-[-0.035em] text-lime">{STARTING_RATING}</span>
            </div>
          </section>

          <div className="overflow-hidden rounded-[20px] border border-line bg-surface">
            <div className="flex items-center gap-3 border-b border-line px-3.5 py-3">
              <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-background">
                <TicketIcon className="size-[19px]" />
              </span>
              <span className="flex flex-col gap-0.5">
                <span className="text-[15px] font-semibold">One entry is one match</span>
                <span className="text-[13px] text-muted">Want another match here? Join again, up to 3 entries.</span>
              </span>
            </div>
            <div className="flex items-center gap-3 px-3.5 py-3">
              <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-background">
                <ShieldCheckIcon className="size-[19px]" />
              </span>
              <span className="flex flex-col gap-0.5">
                <span className="text-[15px] font-semibold">Full refund if we can&rsquo;t pair you</span>
                <span className="text-[13px] text-muted">Sent back automatically before the event.</span>
              </span>
            </div>
          </div>

          <p className="flex items-center gap-2 rounded-2xl bg-line/60 px-3.5 py-3 text-[13px] font-semibold">
            <ClockIcon className="size-4 shrink-0" />
            We hold your spot for 10 minutes while you pay.
          </p>
          {error && (
            <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
              {error}
            </p>
          )}
        </div>
      </div>

      <footer className="fixed bottom-0 left-1/2 z-10 flex w-full max-w-[480px] -translate-x-1/2 items-center gap-4 border-t border-line bg-surface px-5 pt-3 pb-[max(28px,env(safe-area-inset-bottom))]">
        <div className="flex flex-col">
          <span className="text-xs text-muted">UPI, card or netbanking</span>
          <span className="text-[22px] leading-tight font-semibold tracking-[-0.035em]">{rupees(e.feePaise)}</span>
        </div>
        <button
          type="button"
          onClick={() => void pay()}
          disabled={busy}
          className="flex min-h-[54px] flex-1 items-center justify-center gap-2.5 rounded-2xl bg-primary text-base font-semibold text-on-primary disabled:opacity-50"
        >
          <LockIcon className="size-4" />
          {payEntry.busy ? "Paying…" : busy ? "Holding your spot…" : `Pay ${rupees(e.feePaise)}`}
        </button>
      </footer>

      <BottomSheet
        hash="partner"
        title="Choose a partner"
        description="They need their own entry in this event for the same format."
      >
        <div className="pb-4">
          <PlayerSearch
            id="partner-search"
            onPick={(u) => {
              setPartner(u);
              clear("partner");
              partnerSheet.close();
            }}
          />
        </div>
      </BottomSheet>
    </main>
  );
}

/** Join an event: pick formats (and a partner for doubles), then pay. */
export default function JoinPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [event, setEvent] = useState<BadmintonEvent | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    me()
      .then((u) => {
        if (!u.profileComplete) {
          router.replace("/setup-profile");
          return;
        }
        return getEvent(id).then(setEvent);
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Couldn't load the event.");
      });
  }, [id, router]);

  if (!event) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background px-6 text-center">
        <p role={error ? "alert" : undefined} className="text-sm text-muted">
          {error ?? "Loading…"}
        </p>
      </main>
    );
  }
  return <JoinForm event={event} />;
}
