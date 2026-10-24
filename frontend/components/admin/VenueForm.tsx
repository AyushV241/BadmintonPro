"use client";

import { useState, type FormEvent } from "react";
import { ErrorNote, StickyFooter } from "@/components/admin/AdminShell";
import { MapPreview } from "@/components/events/VenueMap";
import { FieldError } from "@/components/form/FieldError";
import { FieldLabel } from "@/components/form/FieldLabel";
import { Button, Input } from "@/components/ui";
import { ApiError, adminCreateVenue, type AdminVenue } from "@/lib/api";
import { useFormErrors, type FieldErrors } from "@/lib/useFormErrors";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** A number input's text, or null when empty or not a number. */
function decimal(text: string): number | null {
  const n = Number.parseFloat(text);
  return Number.isNaN(n) ? null : n;
}

/** Both coordinates typed and in range: worth showing on a map. */
function validPoint(lat: string, lng: string) {
  const a = decimal(lat);
  const b = decimal(lng);
  return a !== null && b !== null && Math.abs(a) <= 90 && Math.abs(b) <= 180;
}

type VenueField = "name" | "city" | "courts" | "location";

// The server's rules (validateVenue in admin.go), checked here first so the
// message sits next to the field.
const CITY_SLUG = /^[a-z0-9-]+$/;
const MAX_COURTS = 50;

function validateVenue(name: string, city: string, courts: string, lat: string, lng: string): FieldErrors<VenueField> {
  const errors: FieldErrors<VenueField> = {};
  if (!name.trim()) errors.name = "Enter the venue's name.";
  if (!CITY_SLUG.test(city.trim())) errors.city = "Lower case, like bangalore.";
  const n = Number.parseInt(courts, 10);
  if (courts.trim() !== "" && (Number.isNaN(n) || n < 0 || n > MAX_COURTS)) errors.courts = `0 to ${MAX_COURTS}.`;
  const hasLat = lat.trim() !== "";
  const hasLng = lng.trim() !== "";
  if (hasLat !== hasLng) errors.location = "Give both latitude and longitude, or neither.";
  else if (hasLat && !validPoint(lat, lng)) errors.location = "Latitude is −90 to 90 and longitude −180 to 180.";
  return errors;
}

function fieldForServerError(message: string): VenueField | null {
  const m = message.toLowerCase();
  if (m.includes("city")) return "city";
  if (m.includes("courts")) return "courts";
  if (m.includes("lat") || m.includes("lng")) return "location";
  if (m.includes("name")) return "name";
  return null;
}

/**
 * A new venue: name, address, city, courts and its map location. The
 * Create button is pinned to the bottom of the screen.
 */
export function VenueForm({ onCreated }: { onCreated: (v: AdminVenue) => void }) {
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [city, setCity] = useState("bangalore");
  const [lat, setLat] = useState("");
  const [lng, setLng] = useState("");
  const [courts, setCourts] = useState("4");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const { errors, field, show, clear } = useFormErrors<VenueField>();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (show(validateVenue(name, city, courts, lat, lng))) return;
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
    } catch (err) {
      const key = err instanceof ApiError ? fieldForServerError(err.message) : null;
      if (key) show({ [key]: message(err) });
      else setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
    <form id="venue-form" onSubmit={submit} noValidate className="flex flex-col gap-4 rounded-[22px] border border-line bg-surface p-4">
      <div {...field("name")} className="flex flex-col gap-2">
        <FieldLabel htmlFor="venue-name">Name</FieldLabel>
        <Input
          id="venue-name"
          value={name}
          onChange={(v) => {
            setName(v);
            clear("name");
          }}
          placeholder="Koramangala Sports Arena"
          error={errors.name}
        />
      </div>
      <div className="flex flex-col gap-2">
        <FieldLabel htmlFor="venue-address" optional>
          Address
        </FieldLabel>
        <Input id="venue-address" value={address} onChange={setAddress} placeholder="80 Feet Road, Koramangala" />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div {...field("city")} className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-city">City</FieldLabel>
          <Input
            id="venue-city"
            value={city}
            onChange={(v) => {
              setCity(v);
              clear("city");
            }}
            helperText="e.g. bangalore"
            error={errors.city}
          />
        </div>
        <div {...field("courts")} className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-courts">Courts</FieldLabel>
          <Input
            id="venue-courts"
            type="number"
            inputMode="numeric"
            value={courts}
            onChange={(v) => {
              setCourts(v);
              clear("courts");
            }}
            error={errors.courts}
          />
        </div>
      </div>
      <div {...field("location")} className="flex flex-col gap-2">
      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-lat" optional>
            Latitude
          </FieldLabel>
          <Input
            id="venue-lat"
            type="number"
            inputMode="decimal"
            value={lat}
            onChange={(v) => {
              setLat(v);
              clear("location");
            }}
            placeholder="12.9352"
            error={!!errors.location}
          />
        </div>
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="venue-lng" optional>
            Longitude
          </FieldLabel>
          <Input
            id="venue-lng"
            type="number"
            inputMode="decimal"
            value={lng}
            onChange={(v) => {
              setLng(v);
              clear("location");
            }}
            placeholder="77.6245"
            error={!!errors.location}
          />
        </div>
      </div>
      <FieldError id="venue-location-error" message={errors.location} />
      </div>
      <p className="text-xs text-muted">
        Location lets players find the venue by distance and shows it on a map. In Google Maps, long-press the
        venue and copy the numbers that appear.
      </p>
      {validPoint(lat, lng) && (
        <MapPreview lat={decimal(lat)!} lng={decimal(lng)!} title="Map preview of the new venue" />
      )}
      <ErrorNote message={error} />
    </form>
    {/* Outside the card so its spacer doesn't pad the card; `form` still ties it to the form. */}
    <StickyFooter>
      <Button type="submit" form="venue-form" variant="accent" size="lg" fullWidth loading={busy}>
        Create venue
      </Button>
    </StickyFooter>
    </>
  );
}
