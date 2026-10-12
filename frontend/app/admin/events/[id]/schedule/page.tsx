"use client";

import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { AdminShell, ErrorNote } from "@/components/admin/AdminShell";
import { formatName } from "@/components/events/EventBits";
import { Button } from "@/components/ui";
import {
  ApiError,
  adminGetEvent,
  adminGetSchedule,
  adminListEntries,
  adminMarkPaid,
  adminPreviewSchedule,
  adminPublishSchedule,
  type AdminEntry,
  type BadmintonEvent,
  type EntryStatus,
  type Schedule,
  type ScheduledMatch,
} from "@/lib/api";
import { clockTime } from "@/lib/eventTime";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

const statusLabel: Record<EntryStatus, string> = {
  pending_payment: "Waiting for payment",
  confirmed: "Paid",
  standby: "Standby",
  scheduled: "Scheduled",
  cancelled: "Cancelled",
  refunded: "Refunded",
  no_show: "No-show",
};

const statusStyle: Partial<Record<EntryStatus, string>> = {
  confirmed: "bg-lime text-ink",
  scheduled: "bg-foreground/10 text-foreground",
  standby: "bg-[#f5a524]/20 text-foreground",
};

function EntryRow({ entry, onChange }: { entry: AdminEntry; onChange: (e: AdminEntry) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ratings = entry.formats.map((f) => `${formatName(f)} ${entry.ratings[f] ?? 1200}`).join(" · ");
  const status = entry.status === "pending_payment" && entry.holdExpired ? "Hold ran out" : statusLabel[entry.status];

  async function markPaid() {
    setError(null);
    setBusy(true);
    try {
      onChange(await adminMarkPaid(entry.id));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="flex flex-col gap-2 border-b border-line px-4 py-3 last:border-b-0">
      <div className="flex items-start justify-between gap-3">
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate text-[15px] font-semibold">{entry.user.name}</span>
          <span className="truncate text-xs text-muted">
            @{entry.user.username} · {ratings}
            {entry.partner && ` · with @${entry.partner.username}`}
          </span>
          {entry.standbyReason && <span className="text-xs text-muted">Standby: {entry.standbyReason}</span>}
        </span>
        <span
          className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-semibold ${statusStyle[entry.status] ?? "bg-line text-muted"}`}
        >
          {status}
        </span>
      </div>
      {entry.status === "pending_payment" && (
        <Button variant="secondary" size="sm" loading={busy} onClick={() => void markPaid()} className="self-start">
          Mark paid (cash or UPI)
        </Button>
      )}
      <ErrorNote message={error} />
    </li>
  );
}

function MatchCard({ match, timeZone }: { match: ScheduledMatch; timeZone: string }) {
  const [a, b] = match.sides;
  const names = (side: typeof a) => side.players.map((p) => `${p.name} (${p.rating})`).join(" & ");
  return (
    <li className="flex flex-col gap-2 rounded-[20px] border border-line bg-surface p-4">
      <div className="flex items-center justify-between gap-3 text-xs font-semibold tracking-[0.08em] text-muted uppercase">
        <span>
          {clockTime(match.startsAt, timeZone)} · {match.court.label}
        </span>
        <span>{formatName(match.format)}</span>
      </div>
      <p className="text-[15px] leading-snug font-semibold">
        {names(a)} <span className="font-medium text-muted">vs</span> {names(b)}
      </p>
      <p className="text-xs text-muted">
        {match.judge ? `Judge: ${match.judge.name}` : "No judge yet"} · sides {a.rating} vs {b.rating}
      </p>
      {match.flags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {match.flags.map((f) => (
            <span key={f} className="rounded-full bg-danger/10 px-2.5 py-1 text-[11px] font-semibold text-danger">
              {f}
            </span>
          ))}
        </div>
      )}
    </li>
  );
}

function ScheduleView({ schedule, timeZone }: { schedule: Schedule; timeZone: string }) {
  return (
    <div className="flex flex-col gap-3">
      {schedule.warnings.map((w) => (
        <p key={w} className="rounded-xl bg-[#f5a524]/15 px-3 py-2 text-sm">
          {w}
        </p>
      ))}
      {schedule.matches.length === 0 && <p className="text-sm text-muted">No matches: the paid entries don&rsquo;t make a pair yet.</p>}
      <ul className="flex flex-col gap-3">
        {schedule.matches.map((m, i) => (
          <MatchCard key={m.id ?? i} match={m} timeZone={timeZone} />
        ))}
      </ul>
      {schedule.standby.length > 0 && (
        <div className="flex flex-col gap-2 rounded-[20px] border border-line bg-surface p-4">
          <p className="text-[15px] font-semibold">Standby</p>
          {schedule.standby.map((s) => (
            <p key={s.entryId} className="text-sm text-muted">
              {s.user.name} (@{s.user.username}): {s.reason}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

function SchedulePanel({ id }: { id: string }) {
  const [event, setEvent] = useState<BadmintonEvent | null>(null);
  const [entries, setEntries] = useState<AdminEntry[] | null>(null);
  const [published, setPublished] = useState<Schedule | null>(null);
  const [preview, setPreview] = useState<Schedule | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"preview" | "publish" | null>(null);

  useEffect(() => {
    Promise.all([adminGetEvent(id), adminListEntries(id), adminGetSchedule(id)])
      .then(([{ event }, list, saved]) => {
        setEvent(event);
        setEntries(list);
        setPublished(saved);
      })
      .catch((err) => setError(message(err)));
  }, [id]);

  async function run(kind: "preview" | "publish") {
    if (kind === "publish" && !window.confirm("Publish this schedule? Players will see their matches, and it can't be redone yet.")) {
      return;
    }
    setError(null);
    setBusy(kind);
    try {
      if (kind === "preview") {
        setPreview(await adminPreviewSchedule(id));
      } else {
        setPublished(await adminPublishSchedule(id));
        setPreview(null);
        setEntries(await adminListEntries(id));
      }
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(null);
    }
  }

  if (!event || !entries) {
    return error ? <ErrorNote message={error} /> : <p className="text-sm text-muted">Loading…</p>;
  }
  const tz = event.venue.timezone;
  const paid = entries.filter((e) => e.status === "confirmed").length;
  const live = entries.filter((e) => e.status !== "cancelled" && e.status !== "refunded");

  return (
    <div className="flex flex-col gap-6">
      <p className="text-sm text-muted">{event.title}</p>

      <section className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-[17px] font-semibold tracking-[-0.02em]">Entries</h2>
          <span className="text-sm text-muted">
            {paid} paid · {live.length} in total
          </span>
        </div>
        {live.length === 0 ? (
          <p className="text-sm text-muted">No one has joined yet.</p>
        ) : (
          <ul className="overflow-hidden rounded-[22px] border border-line bg-surface">
            {live.map((e) => (
              <EntryRow key={e.id} entry={e} onChange={(u) => setEntries(entries.map((x) => (x.id === u.id ? u : x)))} />
            ))}
          </ul>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-[17px] font-semibold tracking-[-0.02em]">Schedule</h2>
        <ErrorNote message={error} />
        {published ? (
          <>
            <p className="rounded-xl bg-lime/30 px-3 py-2 text-sm font-semibold">
              Published. Players can see their matches.
            </p>
            <ScheduleView schedule={published} timeZone={tz} />
          </>
        ) : (
          <>
            <p className="text-sm text-muted">
              The matcher pairs the paid entries by rating and gives each match a court, a time and a judge. Preview as often as you
              like; publishing saves it.
            </p>
            <Button variant="secondary" size="lg" fullWidth loading={busy === "preview"} onClick={() => void run("preview")}>
              {preview ? "Preview again" : "Preview schedule"}
            </Button>
            {preview && (
              <>
                <ScheduleView schedule={preview} timeZone={tz} />
                {preview.matches.length > 0 && (
                  <Button variant="accent" size="lg" fullWidth loading={busy === "publish"} onClick={() => void run("publish")}>
                    Publish schedule
                  </Button>
                )}
              </>
            )}
          </>
        )}
      </section>
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
