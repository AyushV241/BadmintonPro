// Event times are shown and edited in the venue's time zone, not the viewer's.

function parts(iso: string, timeZone: string, options: Intl.DateTimeFormatOptions) {
  const list = new Intl.DateTimeFormat("en-GB", { timeZone, hourCycle: "h23", ...options }).formatToParts(
    new Date(iso),
  );
  return (type: Intl.DateTimeFormatPartTypes) => list.find((p) => p.type === type)?.value ?? "";
}

/** "2026-10-17", for a date input. */
export function venueDate(iso: string, timeZone: string) {
  const get = parts(iso, timeZone, { year: "numeric", month: "2-digit", day: "2-digit" });
  return `${get("year")}-${get("month")}-${get("day")}`;
}

/** "19:30", for a time input. */
export function venueTime(iso: string, timeZone: string) {
  const get = parts(iso, timeZone, { hour: "2-digit", minute: "2-digit" });
  return `${get("hour")}:${get("minute")}`;
}

/** "Sat 17 Oct · 7:30 PM" */
export function formatWhen(iso: string, timeZone: string) {
  const date = new Intl.DateTimeFormat("en-GB", { timeZone, weekday: "short", day: "numeric", month: "short" })
    .format(new Date(iso))
    .replace(",", "");
  const time = new Intl.DateTimeFormat("en-US", { timeZone, hour: "numeric", minute: "2-digit" }).format(
    new Date(iso),
  );
  return `${date} · ${time}`;
}

/** 5000 → "₹50" */
export function rupees(paise: number) {
  return `₹${(paise / 100).toLocaleString("en-IN", { maximumFractionDigits: 2 })}`;
}

/** Whole slots per court, as the server counts them: shortest format decides. */
export function maxMatchesFor(courts: number, startTime: string, endTime: string, slotMinutes: number[]) {
  const toMinutes = (t: string) => {
    const [h, m] = t.split(":").map(Number);
    return h * 60 + m;
  };
  const length = toMinutes(endTime) - toMinutes(startTime);
  const shortest = Math.min(...slotMinutes.filter((m) => m > 0));
  if (!Number.isFinite(shortest) || length <= 0) return 0;
  return courts * Math.floor(length / shortest);
}
