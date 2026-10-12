export const avatarColors = {
  peach: "#f0b07a",
  blue: "#9cc2f5",
  pink: "#f0a3b8",
  sand: "#e5d38f",
  lime: "#e2fb6c",
} as const;

/** A round initials badge. */
export function Avatar({ text, color, size = 40 }: { text?: string; color: string; size?: number }) {
  return (
    <span
      className="flex shrink-0 items-center justify-center rounded-full font-semibold tracking-[-0.02em] text-ink"
      style={{ width: size, height: size, background: color, fontSize: Math.round(size * 0.36) }}
    >
      {text}
    </span>
  );
}
