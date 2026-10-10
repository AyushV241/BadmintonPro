/** "Arjun Kapoor" → "AK"; falls back to the first letter of `fallback` (e.g. the username). */
export function initials(name: string, fallback = ""): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const letters =
    words.length > 1 ? words[0][0] + words[words.length - 1][0] : (words[0]?.[0] ?? fallback[0] ?? "");
  return letters.toUpperCase();
}
