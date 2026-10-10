"use client";

import { useEffect, useState, type FormEvent } from "react";
import { AdminShell, ErrorNote } from "@/components/admin/AdminShell";
import { FieldLabel } from "@/components/form/FieldLabel";
import { PinIcon } from "@/components/icons";
import { Button, Input } from "@/components/ui";
import { ApiError, adminAddCourt, adminCreateVenue, adminListVenues, type AdminVenue } from "@/lib/api";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** A number input's text, or null when empty or not a number. */
function decimal(text: string): number | null {
  const n = Number.parseFloat(text);
  return Number.isNaN(n) ? null : n;
}

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

function NewVenueForm({ onCreated }: { onCreated: (v: AdminVenue) => void }) {
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [city, setCity] = useState("bangalore");
  const [lat, setLat] = useState("");
  const [lng, setLng] = useState("");
  const [courts, setCourts] = useState("4");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const venue = await adminCreateVenue({
        name,
        address,
        city,
        lat: decimal(lat),
        lng: decimal(lng),
        courts: Number.parseInt(courts, 10) || 0,
      });
      onCreated(venue);
      setName("");
      setAddress("");
      setLat("");
      setLng("");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4 rounded-[22px] border border-line bg-surface p-4">
      <h2 className="text-[17px] font-semibold tracking-[-0.02em]">New venue</h2>
      <div className="flex flex-col gap-2">
        <FieldLabel htmlFor="venue-name">Name</FieldLabel>
        <Input id="venue-name" value={name} onChange={setName} placeholder="Koramangala Sports Arena" required />
      </div>
      <div className="flex flex-col gap-2">
        <FieldLabel htmlFor="venue-address" optional>
          Address
        </FieldLabel>
        <Input id="venue-address" value={address} onChange={setAddress} placeholder="80 Feet Road, Koramangala" />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-city">City</FieldLabel>
          <Input id="venue-city" value={city} onChange={setCity} helperText="e.g. bangalore" required />
        </div>
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-courts">Courts</FieldLabel>
          <Input id="venue-courts" type="number" inputMode="numeric" value={courts} onChange={setCourts} />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-lat" optional>
            Latitude
          </FieldLabel>
          <Input id="venue-lat" type="number" inputMode="decimal" value={lat} onChange={setLat} placeholder="12.9352" />
        </div>
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-lng" optional>
            Longitude
          </FieldLabel>
          <Input id="venue-lng" type="number" inputMode="decimal" value={lng} onChange={setLng} placeholder="77.6245" />
        </div>
      </div>
      <p className="text-xs text-muted">
        Location lets players find the venue by distance. Copy it from a map app.
      </p>
      <ErrorNote message={error} />
      <Button type="submit" variant="accent" size="lg" fullWidth loading={busy}>
        Create venue
      </Button>
    </form>
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
      <ul className="flex flex-col gap-3">
        {venues.map((v) => (
          <VenueCard
            key={v.id}
            venue={v}
            onChange={(updated) => setVenues(venues.map((x) => (x.id === updated.id ? updated : x)))}
          />
        ))}
      </ul>
      <NewVenueForm onCreated={(v) => setVenues([...venues, v])} />
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
