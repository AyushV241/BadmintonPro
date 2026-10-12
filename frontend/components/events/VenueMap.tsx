import { PinIcon } from "@/components/icons";
import type { Venue } from "@/lib/api";

// Optional. With a key the official Maps Embed API is used (free, no usage
// limit); without one, the keyless embed still shows the pin.
const embedKey = process.env.NEXT_PUBLIC_GOOGLE_MAPS_EMBED_KEY;

function embedUrl(lat: number, lng: number) {
  const q = `${lat},${lng}`;
  return embedKey
    ? `https://www.google.com/maps/embed/v1/place?key=${encodeURIComponent(embedKey)}&q=${q}&zoom=15`
    : `https://maps.google.com/maps?q=${q}&z=15&output=embed`;
}

/** Just the map, e.g. to check a pin while typing coordinates. */
export function MapPreview({ lat, lng, title }: { lat: number; lng: number; title: string }) {
  return (
    <iframe
      title={title}
      src={embedUrl(lat, lng)}
      loading="lazy"
      referrerPolicy="no-referrer-when-downgrade"
      className="block h-[180px] w-full rounded-2xl border border-line"
    />
  );
}

/** Opens Google Maps (the app on phones) with directions to the venue. */
export function directionsUrl(venue: Venue) {
  return venue.lat !== null && venue.lng !== null
    ? `https://www.google.com/maps/dir/?api=1&destination=${venue.lat},${venue.lng}`
    : null;
}

/** The venue on a Google map, with its address and a Directions button. Nothing without coordinates. */
export function VenueMap({ venue }: { venue: Venue }) {
  const directions = directionsUrl(venue);
  if (venue.lat === null || venue.lng === null || !directions) return null;

  return (
    <section aria-label="Location" className="overflow-hidden rounded-[20px] border border-line bg-surface">
      <iframe
        title={`Map of ${venue.name}`}
        src={embedUrl(venue.lat, venue.lng)}
        loading="lazy"
        referrerPolicy="no-referrer-when-downgrade"
        className="block h-[180px] w-full border-0"
      />
      <div className="flex items-center gap-3 px-3.5 py-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-background">
          <PinIcon className="size-[19px]" />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-[15px] font-semibold">{venue.name}</span>
          {venue.address && <span className="truncate text-[13px] text-muted">{venue.address}</span>}
        </span>
        <a
          href={directions}
          target="_blank"
          rel="noreferrer"
          className="flex min-h-11 shrink-0 items-center rounded-xl bg-primary px-3.5 text-[13px] font-semibold text-on-primary"
        >
          Directions
        </a>
      </div>
    </section>
  );
}
