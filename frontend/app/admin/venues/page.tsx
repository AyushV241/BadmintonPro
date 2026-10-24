"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { AdminShell, ErrorNote, StickyFooter } from "@/components/admin/AdminShell";
import { FieldLabel } from "@/components/form/FieldLabel";
import { PinIcon, PlusIcon } from "@/components/icons";
import { Button, Input } from "@/components/ui";
import { ApiError, adminAddCourt, adminListVenues, type AdminVenue } from "@/lib/api";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

function VenueCard({ venue, onChange }: { venue: AdminVenue; onChange: (v: AdminVenue) => void }) {
  const [label, setLabel] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const inputId = `court-${venue.id}`;

  async function addCourt(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const court = await adminAddCourt(venue.id, label.trim());
      onChange({ ...venue, courts: [...venue.courts, court] });
      setLabel("");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="flex flex-col gap-3 rounded-[22px] border border-line bg-surface p-4">
      <div className="flex flex-col gap-0.5">
        <span className="text-[16px] font-semibold tracking-[-0.02em]">{venue.name}</span>
        <span className="flex items-center gap-1 text-sm text-muted">
          <PinIcon className="size-3.5 shrink-0" />
          {[venue.address, venue.city].filter(Boolean).join(", ")}
        </span>
        {venue.lat === null && (
          <span className="text-xs text-muted">No map location: it won&rsquo;t show in distance searches.</span>
        )}
      </div>
      <div className="flex flex-wrap gap-2">
        {venue.courts.map((c) => (
          <span
            key={c.id}
            className={`rounded-full border border-line px-3 py-1.5 text-xs font-semibold ${c.active ? "" : "opacity-40"}`}
          >
            {c.label}
          </span>
        ))}
        {venue.courts.length === 0 && <span className="text-sm text-muted">No courts yet.</span>}
      </div>
      <form onSubmit={addCourt} className="flex flex-col gap-2">
        <FieldLabel htmlFor={inputId}>Add a court</FieldLabel>
        <div className="flex gap-2">
          <Input
            id={inputId}
            value={label}
            onChange={setLabel}
            placeholder={`Court ${venue.courts.length + 1}`}
            maxLength={40}
          />
          <Button type="submit" variant="secondary" loading={busy} disabled={!label.trim()}>
            Add
          </Button>
        </div>
      </form>
      <ErrorNote message={error} />
    </li>
  );
}

function Venues() {
  const [venues, setVenues] = useState<AdminVenue[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    adminListVenues()
      .then(setVenues)
      .catch((err) => setError(message(err)));
  }, []);

  if (error) return <ErrorNote message={error} />;
  if (!venues) return <p className="text-sm text-muted">Loading…</p>;

  return (
    <div className="flex flex-col gap-4">
      {venues.length === 0 && (
        <p className="rounded-[22px] border border-line bg-surface p-5 text-sm text-muted">
          No venues yet. Create one to start adding events.
        </p>
      )}
      <ul className="flex flex-col gap-3">
        {venues.map((v) => (
          <VenueCard
            key={v.id}
            venue={v}
            onChange={(updated) => setVenues(venues.map((x) => (x.id === updated.id ? updated : x)))}
          />
        ))}
      </ul>
      <StickyFooter>
        <Link
          href="/admin/venues/new"
          className="flex min-h-14 items-center justify-center gap-2 rounded-2xl bg-lime text-base font-semibold text-ink"
        >
          <PlusIcon className="size-5" />
          Create venue
        </Link>
      </StickyFooter>
    </div>
  );
}

export default function AdminVenuesPage() {
  return (
    <AdminShell title="Venues" back={{ href: "/admin", label: "Events" }}>
      {() => <Venues />}
    </AdminShell>
  );
}
