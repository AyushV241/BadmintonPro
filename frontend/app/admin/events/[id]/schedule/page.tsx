"use client";

import { useParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { AdminShell, ErrorNote, StickyFooter } from "@/components/admin/AdminShell";
import { formatName } from "@/components/events/EventBits";
import { BottomSheet, Button, useBottomSheet } from "@/components/ui";
import {
  ApiError,
  adminCancelMatch,
  adminCreateMatch,
  adminFreeSlots,
  adminGetEvent,
  adminGetSchedule,
  adminListChanges,
  adminListEntries,
  adminMarkPaid,
  adminMoveMatch,
  adminPreviewSchedule,
  adminPublishSchedule,
  adminReplacePlayer,
  adminSuggestSlot,
  type AdminEntry,
  type BadmintonEvent,
  type EntryStatus,
  type EventFormat,
  type FreeSlot,
  type MatchChange,
  type ReplaceOutcome,
  type Schedule,
  type ScheduledMatch,
  type SuggestedSlot,
} from "@/lib/api";
import { clockTime, formatWhen } from "@/lib/eventTime";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** Paid and not in a match yet: the players the admin can pair or swap in. */
const isWaiting = (e: AdminEntry) => e.kind === "open" && (e.status === "confirmed" || e.status === "standby") && !e.matchId;

/** A match that can still be edited: saved, and not under way or over. */
const isEditable = (m: ScheduledMatch) => !!m.id && (m.status === undefined || m.status === "scheduled" || m.status === "ready");

const playersNeeded = (f: EventFormat) => (f === "singles" ? 2 : 4);

const statusLabel: Record<EntryStatus, string> = {
  pending_payment: "Waiting for payment",
  confirmed: "Paid",
  standby: "Standby",
  scheduled: "Scheduled",
  cancelled: "Cancelled",
  refunded: "Refunded",
  no_show: "No-show",
};

function Section({ title, aside, children }: { title: string; aside?: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-[17px] font-semibold tracking-[-0.02em]">{title}</h2>
        {aside && <span className="text-sm text-muted">{aside}</span>}
      </div>
      {children}
    </section>
  );
}

function Flags({ flags }: { flags: string[] }) {
  if (flags.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {flags.map((f) => (
        <span key={f} className="rounded-full bg-danger/10 px-2.5 py-1 text-[11px] font-semibold text-danger">
          {f}
        </span>
      ))}
    </div>
  );
}

function MatchCard({
  match,
  timeZone,
  onMove,
  onReplace,
  onCancel,
}: {
  match: ScheduledMatch;
  timeZone: string;
  onMove?: () => void;
  onReplace?: () => void;
  onCancel?: () => void;
}) {
  const [a, b] = match.sides;
  const names = (side: typeof a) => side.players.map((p) => `${p.name} (${p.rating})`).join(" & ");
  const editable = isEditable(match) && (onMove || onReplace || onCancel);
  const challenge = !!match.challengeId;
  return (
    <li className="flex flex-col gap-2 rounded-[20px] border border-line bg-surface p-4">
      <div className="flex items-center justify-between gap-3 text-xs font-semibold tracking-[0.08em] text-muted uppercase">
        <span>
          {clockTime(match.startsAt, timeZone)} · {match.court.label}
        </span>
        <span>
          {challenge ? "Challenge · " : ""}
          {formatName(match.format)}
        </span>
      </div>
      <p className="text-[15px] leading-snug font-semibold">
        {names(a)} <span className="font-medium text-muted">vs</span> {names(b)}
      </p>
      <p className="text-xs text-muted">
        {match.judge ? `Judge: ${match.judge.name}` : "No judge yet"} · sides {a.rating} vs {b.rating}
      </p>
      <Flags flags={match.flags} />
      {editable && (
        <div className="flex flex-wrap gap-2 pt-1">
          {onMove && (
            <Button variant="secondary" size="sm" onClick={onMove}>
              Change time
            </Button>
          )}
          {/* Challenge pairs were chosen and paid for by both players. */}
          {!challenge && onReplace && (
            <Button variant="secondary" size="sm" onClick={onReplace}>
              Replace player
            </Button>
          )}
          {!challenge && onCancel && (
            <Button variant="ghost" size="sm" className="text-danger" onClick={onCancel}>
              Cancel match
            </Button>
          )}
        </div>
      )}
    </li>
  );
}

function PlayerLine({ entry }: { entry: AdminEntry }) {
  const ratings = entry.formats.map((f) => `${formatName(f)} ${entry.ratings[f] ?? 1200}`).join(" · ");
  return (
    <span className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate text-[15px] font-semibold">{entry.user.name}</span>
      <span className="truncate text-xs text-muted">
        @{entry.user.username} · {ratings}
        {entry.partner && ` · with @${entry.partner.username}`}
      </span>
      {entry.standbyReason && <span className="text-xs text-muted">Standby: {entry.standbyReason}</span>}
    </span>
  );
}

function UnpaidRow({ entry, onChange }: { entry: AdminEntry; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const label = entry.status === "pending_payment" && entry.holdExpired ? "Hold ran out" : statusLabel[entry.status];

  async function markPaid() {
    setError(null);
    setBusy(true);
    try {
      await adminMarkPaid(entry.id);
      onChange();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="flex flex-col gap-2 border-b border-line px-4 py-3 last:border-b-0">
      <div className="flex items-start justify-between gap-3">
        <PlayerLine entry={entry} />
        <span className="shrink-0 rounded-full bg-line px-2.5 py-1 text-[11px] font-semibold text-muted">{label}</span>
      </div>
      {entry.status === "pending_payment" && entry.kind === "open" && (
        <Button variant="secondary" size="sm" loading={busy} onClick={() => void markPaid()} className="self-start">
          Mark paid (cash or UPI)
        </Button>
      )}
      <ErrorNote message={error} />
    </li>
  );
}

/** Court-free times, as chips; the selected one is filled. */
function SlotPicker({
  slots,
  timeZone,
  selected,
  onPick,
}: {
  slots: FreeSlot[] | null;
  timeZone: string;
  selected: FreeSlot | null;
  onPick: (s: FreeSlot) => void;
}) {
  if (!slots) return <p className="text-sm text-muted">Loading free times…</p>;
  if (slots.length === 0) return <p className="text-sm text-muted">No court is free at any time in this event.</p>;
  const key = (s: FreeSlot) => `${s.court.id}|${s.startsAt}`;
  return (
    <div role="group" aria-label="Free courts and times" className="flex max-h-56 flex-wrap gap-2 overflow-y-auto">
      {slots.map((s) => {
        const on = !!selected && key(selected) === key(s);
        return (
          <button
            key={key(s)}
            type="button"
            aria-pressed={on}
            onClick={() => onPick(s)}
            className={`min-h-11 rounded-xl px-3 text-sm font-semibold ${on ? "bg-primary text-on-primary" : "border border-line bg-surface"}`}
          >
            {clockTime(s.startsAt, timeZone)} · {s.court.label}
          </button>
        );
      })}
    </div>
  );
}

function CreateMatchSheet({
  eventId,
  timeZone,
  picked,
  formats,
  onDone,
}: {
  eventId: string;
  timeZone: string;
  picked: AdminEntry[];
  formats: EventFormat[];
  onDone: () => void;
}) {
  const sheet = useBottomSheet("create-match");
  // Formats every picked player allows, that the event offers, for this many players.
  const options = formats.filter((f) => playersNeeded(f) === picked.length && picked.every((e) => e.formats.includes(f)));
  const [format, setFormat] = useState<EventFormat | null>(options[0] ?? null);
  const [sideA, setSideA] = useState<string[]>(() => picked.slice(0, picked.length / 2).map((e) => e.id));
  const [suggestion, setSuggestion] = useState<SuggestedSlot | null>(null);
  const [slots, setSlots] = useState<FreeSlot[] | null>(null);
  const [chosen, setChosen] = useState<FreeSlot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const ids = picked.map((e) => e.id);
  const sideB = ids.filter((id) => !sideA.includes(id));
  const sidesOk = format !== null && sideA.length === playersNeeded(format) / 2;

  // Ask where it would go: the earliest suitable slot, or the one the admin picked.
  useEffect(() => {
    if (!format || !sidesOk) return;
    let cancelled = false;
    adminSuggestSlot(eventId, format, ids, chosen ? { courtId: chosen.court.id, startsAt: chosen.startsAt } : undefined)
      .then((s) => !cancelled && (setSuggestion(s), setError(null)))
      .catch((err) => !cancelled && (setSuggestion(null), setError(message(err))));
    return () => {
      cancelled = true;
    };
    // ids is derived from picked; picked changes when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventId, format, sidesOk, chosen, picked]);

  async function create() {
    if (!format) return;
    setError(null);
    setBusy(true);
    try {
      await adminCreateMatch(eventId, {
        format,
        sideA,
        sideB,
        ...(chosen ? { courtId: chosen.court.id, startsAt: chosen.startsAt } : {}),
      });
      sheet.close();
      onDone();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <BottomSheet
      hash="create-match"
      title="Create a match"
      description={picked.map((e) => e.user.name).join(", ")}
      footer={
        <Button variant="accent" size="lg" fullWidth loading={busy} disabled={!suggestion || !sidesOk} onClick={() => void create()}>
          Create match
        </Button>
      }
    >
      <div className="flex flex-col gap-4 pb-2">
        {options.length === 0 ? (
          <p className="text-sm text-muted">
            These players don&rsquo;t share a format for {picked.length} players. Pick 2 for singles, or 4 who all allow doubles or mixed.
          </p>
        ) : (
          <>
            {options.length > 1 && (
              <div role="group" aria-label="Format" className="flex flex-wrap gap-2">
                {options.map((f) => (
                  <button
                    key={f}
                    type="button"
                    aria-pressed={f === format}
                    onClick={() => setFormat(f)}
                    className={`min-h-11 rounded-xl px-3.5 text-sm font-semibold ${f === format ? "bg-primary text-on-primary" : "border border-line bg-surface"}`}
                  >
                    {formatName(f)}
                  </button>
                ))}
              </div>
            )}
            {format !== "singles" && (
              <div className="flex flex-col gap-2">
                <p className="text-[13px] font-semibold text-muted">Teams: tap a player to switch side</p>
                {picked.map((e) => {
                  const a = sideA.includes(e.id);
                  return (
                    <button
                      key={e.id}
                      type="button"
                      onClick={() => setSideA(a ? sideA.filter((x) => x !== e.id) : [...sideA, e.id])}
                      className="flex min-h-12 items-center justify-between rounded-xl border border-line bg-surface px-3 text-left text-sm"
                    >
                      <span className="font-semibold">{e.user.name}</span>
                      <span className={`rounded-full px-2.5 py-1 text-xs font-semibold ${a ? "bg-lime text-ink" : "bg-line"}`}>
                        Side {a ? "A" : "B"}
                      </span>
                    </button>
                  );
                })}
                {!sidesOk && <p className="text-sm text-danger">Each side needs two players.</p>}
              </div>
            )}
            {suggestion && (
              <div className="flex flex-col gap-1 rounded-2xl bg-line/50 px-3.5 py-3">
                <span className="text-[15px] font-semibold">
                  {formatWhen(suggestion.startsAt, timeZone)} · {suggestion.court.label}
                </span>
                <span className="text-xs text-muted">
                  {suggestion.judge ? `Judge: ${suggestion.judge.name}` : "No judge free then"}
                </span>
                <Flags flags={suggestion.flags} />
              </div>
            )}
            <details
              onToggle={(ev) => {
                if ((ev.target as HTMLDetailsElement).open && !slots && format) {
                  adminFreeSlots(eventId, format)
                    .then(setSlots)
                    .catch(() => setSlots([]));
                }
              }}
            >
              <summary className="min-h-11 cursor-pointer py-2 text-sm font-semibold underline underline-offset-4">
                Pick another court or time
              </summary>
              <div className="pt-2">
                <SlotPicker slots={slots} timeZone={timeZone} selected={chosen} onPick={setChosen} />
              </div>
            </details>
          </>
        )}
        <ErrorNote message={error} />
      </div>
    </BottomSheet>
  );
}

function ReplaceSheet({
  match,
  waiting,
  onDone,
}: {
  match: ScheduledMatch | null;
  waiting: AdminEntry[];
  onDone: () => void;
}) {
  const sheet = useBottomSheet("replace-player");
  const players = match?.sides.flatMap((s) => s.players) ?? [];
  const [out, setOut] = useState<string | null>(null);
  const [incoming, setIncoming] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<ReplaceOutcome>("no_show");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const candidates = match ? waiting.filter((e) => e.formats.includes(match.format)) : [];

  async function replace() {
    if (!match?.id || !out || !incoming) return;
    setError(null);
    setBusy(true);
    try {
      await adminReplacePlayer(match.id, out, incoming, outcome);
      sheet.close();
      onDone();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  const radio = (on: boolean) =>
    `flex min-h-12 w-full items-center justify-between gap-3 rounded-xl border px-3 text-left text-sm ${on ? "border-foreground bg-surface" : "border-line bg-surface"}`;
  const dot = (on: boolean) => (
    <span aria-hidden="true" className={`size-5 shrink-0 rounded-full ${on ? "border-[6px] border-foreground" : "border-2 border-line"}`} />
  );

  return (
    <BottomSheet
      hash="replace-player"
      title="Replace a player"
      description="Swap in a paid player who's waiting for a match."
      footer={
        <Button variant="accent" size="lg" fullWidth loading={busy} disabled={!out || !incoming} onClick={() => void replace()}>
          Replace player
        </Button>
      }
    >
      <div className="flex flex-col gap-4 pb-2">
        <div role="radiogroup" aria-label="Who comes out" className="flex flex-col gap-2">
          <p className="text-[13px] font-semibold text-muted">Who comes out</p>
          {players.map((p) => (
            <button key={p.entryId} type="button" role="radio" aria-checked={out === p.entryId} onClick={() => setOut(p.entryId)} className={radio(out === p.entryId)}>
              <span className="font-semibold">
                {p.name} <span className="font-normal text-muted">· {p.rating}</span>
              </span>
              {dot(out === p.entryId)}
            </button>
          ))}
        </div>
        <div role="radiogroup" aria-label="What happens to them" className="flex flex-col gap-2">
          <p className="text-[13px] font-semibold text-muted">What happens to them</p>
          <button type="button" role="radio" aria-checked={outcome === "no_show"} onClick={() => setOutcome("no_show")} className={radio(outcome === "no_show")}>
            <span className="flex flex-col">
              <span className="font-semibold">No-show</span>
              <span className="text-xs text-muted">They didn&rsquo;t turn up, and keep no refund.</span>
            </span>
            {dot(outcome === "no_show")}
          </button>
          <button type="button" role="radio" aria-checked={outcome === "back_to_pool"} onClick={() => setOutcome("back_to_pool")} className={radio(outcome === "back_to_pool")}>
            <span className="flex flex-col">
              <span className="font-semibold">Back to waiting</span>
              <span className="text-xs text-muted">They keep their entry and wait for another match.</span>
            </span>
            {dot(outcome === "back_to_pool")}
          </button>
        </div>
        <div role="radiogroup" aria-label="Who comes in" className="flex flex-col gap-2">
          <p className="text-[13px] font-semibold text-muted">Who comes in</p>
          {candidates.length === 0 && (
            <p className="text-sm text-muted">No paid player is waiting for {match ? formatName(match.format).toLowerCase() : "this format"}.</p>
          )}
          {candidates.map((e) => (
            <button key={e.id} type="button" role="radio" aria-checked={incoming === e.id} onClick={() => setIncoming(e.id)} className={radio(incoming === e.id)}>
              <PlayerLine entry={e} />
              {dot(incoming === e.id)}
            </button>
          ))}
        </div>
        <ErrorNote message={error} />
      </div>
    </BottomSheet>
  );
}

function MoveSheet({
  eventId,
  match,
  timeZone,
  onDone,
}: {
  eventId: string;
  match: ScheduledMatch | null;
  timeZone: string;
  onDone: () => void;
}) {
  const sheet = useBottomSheet("move-match");
  const [slots, setSlots] = useState<FreeSlot[] | null>(null);
  const [chosen, setChosen] = useState<FreeSlot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!match?.id) return;
    let cancelled = false;
    adminFreeSlots(eventId, match.format, match.id)
      .then((s) => !cancelled && setSlots(s))
      .catch((err) => !cancelled && (setSlots([]), setError(message(err))));
    return () => {
      cancelled = true;
    };
  }, [eventId, match]);

  async function move() {
    if (!match?.id || !chosen) return;
    setError(null);
    setBusy(true);
    try {
      await adminMoveMatch(match.id, chosen.court.id, chosen.startsAt);
      sheet.close();
      onDone();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <BottomSheet
      hash="move-match"
      title="Change time or court"
      description={match ? `Now ${formatWhen(match.startsAt, timeZone)} · ${match.court.label}` : undefined}
      footer={
        <Button variant="accent" size="lg" fullWidth loading={busy} disabled={!chosen} onClick={() => void move()}>
          Move match
        </Button>
      }
    >
      <div className="flex flex-col gap-3 pb-2">
        <p className="text-sm text-muted">
          Free courts and times. Players never end up back to back; that&rsquo;s checked when you move it.
        </p>
        <SlotPicker slots={slots} timeZone={timeZone} selected={chosen} onPick={setChosen} />
        <ErrorNote message={error} />
      </div>
    </BottomSheet>
  );
}

const changeVerb: Record<MatchChange["action"], string> = {
  create: "created a match",
  replace: "replaced a player",
  move: "moved a match",
  cancel: "cancelled a match",
};

function SchedulePanel({ id }: { id: string }) {
  const [event, setEvent] = useState<BadmintonEvent | null>(null);
  const [entries, setEntries] = useState<AdminEntry[] | null>(null);
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [changes, setChanges] = useState<MatchChange[]>([]);
  const [preview, setPreview] = useState<Schedule | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"preview" | "publish" | null>(null);
  const [picked, setPicked] = useState<string[]>([]);
  const [target, setTarget] = useState<ScheduledMatch | null>(null);
  const createSheet = useBottomSheet("create-match");
  const replaceSheet = useBottomSheet("replace-player");
  const moveSheet = useBottomSheet("move-match");

  const reload = useCallback(async () => {
    const [list, saved, log] = await Promise.all([
      adminListEntries(id),
      adminGetSchedule(id),
      adminListChanges(id).catch(() => [] as MatchChange[]),
    ]);
    setEntries(list);
    setSchedule(saved);
    setChanges(log);
    setPicked((p) => p.filter((x) => list.some((e) => e.id === x && isWaiting(e))));
  }, [id]);

  useEffect(() => {
    Promise.all([adminGetEvent(id), adminListEntries(id), adminGetSchedule(id), adminListChanges(id).catch(() => [] as MatchChange[])])
      .then(([{ event }, list, saved, log]) => {
        setEvent(event);
        setEntries(list);
        setSchedule(saved);
        setChanges(log);
      })
      .catch((err) => setError(message(err)));
  }, [id]);

  const waiting = useMemo(() => entries?.filter(isWaiting) ?? [], [entries]);
  const pickedEntries = waiting.filter((e) => picked.includes(e.id));

  async function run(kind: "preview" | "publish") {
    if (kind === "publish" && !window.confirm("Publish this schedule? Players will see their matches.")) return;
    setError(null);
    setBusy(kind);
    try {
      if (kind === "preview") setPreview(await adminPreviewSchedule(id));
      else {
        await adminPublishSchedule(id);
        setPreview(null);
        await reload();
      }
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(null);
    }
  }

  async function cancel(m: ScheduledMatch) {
    if (!m.id || !window.confirm("Cancel this match? Its players go back to waiting for a match.")) return;
    setError(null);
    try {
      await adminCancelMatch(m.id);
      await reload();
    } catch (err) {
      setError(message(err));
    }
  }

  if (!event || !entries) {
    return error ? <ErrorNote message={error} /> : <p className="text-sm text-muted">Loading…</p>;
  }
  const tz = event.venue.timezone;
  const matches = (schedule?.matches ?? []).filter((m) => m.status !== "cancelled");
  const others = entries.filter((e) => !isWaiting(e) && e.status !== "scheduled" && e.status !== "cancelled" && e.status !== "refunded");
  const done = () => void reload().catch((err) => setError(message(err)));

  return (
    <div className="flex flex-col gap-7">
      <div className="flex flex-col gap-1">
        <p className="text-[15px] font-semibold">{event.title}</p>
        <p className="text-sm text-muted">
          {formatWhen(event.startsAt, tz)} · {event.venue.name}
        </p>
      </div>
      <ErrorNote message={error} />

      <Section title="Matches" aside={schedule?.publishedAt ? "Published" : matches.length ? "Not published" : undefined}>
        {matches.length === 0 ? (
          <p className="text-sm text-muted">No matches yet. Pair players below, or run the matcher.</p>
        ) : (
          <ul className="flex flex-col gap-3">
            {matches.map((m) => (
              <MatchCard
                key={m.id}
                match={m}
                timeZone={tz}
                onMove={() => {
                  setTarget(m);
                  moveSheet.open();
                }}
                onReplace={() => {
                  setTarget(m);
                  replaceSheet.open();
                }}
                onCancel={() => void cancel(m)}
              />
            ))}
          </ul>
        )}
      </Section>

      <Section title="Waiting for a match" aside={waiting.length ? `${waiting.length} paid` : undefined}>
        {waiting.length === 0 ? (
          <p className="text-sm text-muted">Every paid player has a match.</p>
        ) : (
          <>
            <p className="text-sm text-muted">Tick 2 players for singles, or 4 for doubles, to pair them by hand.</p>
            <ul className="overflow-hidden rounded-[22px] border border-line bg-surface">
              {waiting.map((e) => {
                const on = picked.includes(e.id);
                return (
                  <li key={e.id} className="border-b border-line last:border-b-0">
                    <label className="flex min-h-16 cursor-pointer items-center gap-3 px-4 py-3">
                      <input
                        type="checkbox"
                        checked={on}
                        onChange={() => setPicked(on ? picked.filter((x) => x !== e.id) : [...picked, e.id])}
                        className="size-5 shrink-0 accent-[#151712] dark:accent-[#e2fb6c]"
                      />
                      <PlayerLine entry={e} />
                    </label>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </Section>

      {!schedule?.publishedAt && (
        <Section title="Matcher">
          <p className="text-sm text-muted">
            Pairs every waiting player by rating and gives each match a court, a time and a judge, around the matches above.
            Preview as often as you like; publishing saves it.
          </p>
          <Button variant="secondary" size="lg" fullWidth loading={busy === "preview"} onClick={() => void run("preview")}>
            {preview ? "Preview again" : "Preview schedule"}
          </Button>
          {preview && (
            <>
              {preview.warnings.map((w) => (
                <p key={w} className="rounded-xl bg-[#f5a524]/15 px-3 py-2 text-sm">
                  {w}
                </p>
              ))}
              <ul className="flex flex-col gap-3">
                {preview.matches.map((m, i) => (
                  <MatchCard key={i} match={m} timeZone={tz} />
                ))}
              </ul>
              {preview.matches.length > 0 && (
                <Button variant="accent" size="lg" fullWidth loading={busy === "publish"} onClick={() => void run("publish")}>
                  Publish schedule
                </Button>
              )}
            </>
          )}
        </Section>
      )}

      {others.length > 0 && (
        <Section title="Not paid yet">
          <ul className="overflow-hidden rounded-[22px] border border-line bg-surface">
            {others.map((e) => (
              <UnpaidRow key={e.id} entry={e} onChange={done} />
            ))}
          </ul>
        </Section>
      )}

      {changes.length > 0 && (
        <Section title="Changes">
          <ul className="flex flex-col gap-2">
            {changes.map((c) => (
              <li key={c.id} className="text-sm">
                <span className="font-semibold">{c.admin.name}</span> {changeVerb[c.action]}
                <span className="text-muted"> · {formatWhen(c.createdAt, tz)}</span>
              </li>
            ))}
          </ul>
        </Section>
      )}

      {picked.length > 0 && (
        <StickyFooter>
          <Button variant="accent" size="lg" fullWidth onClick={createSheet.open}>
            Create match ({picked.length} picked)
          </Button>
        </StickyFooter>
      )}

      {/* Keyed so each opening starts fresh. */}
      <CreateMatchSheet key={picked.join(",")} eventId={id} timeZone={tz} picked={pickedEntries} formats={event.formats} onDone={() => {
        setPicked([]);
        done();
      }} />
      <ReplaceSheet key={`r-${target?.id}`} match={target} waiting={waiting} onDone={done} />
      <MoveSheet key={`m-${target?.id}`} eventId={id} match={target} timeZone={tz} onDone={done} />
    </div>
  );
}

export default function AdminSchedulePage() {
  const { id } = useParams<{ id: string }>();
  return (
    <AdminShell title="Entries & schedule" back={{ href: `/admin/events/${id}`, label: "Event" }}>
      {() => <SchedulePanel id={id} />}
    </AdminShell>
  );
}
