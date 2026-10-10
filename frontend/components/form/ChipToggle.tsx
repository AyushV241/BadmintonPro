import type { ReactNode } from "react";

/** A pill that toggles on and off, e.g. a format or a court to include. */
export function ChipToggle({
  pressed,
  onToggle,
  children,
  disabled,
}: {
  pressed: boolean;
  onToggle: () => void;
  children: ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onToggle}
      className={`min-h-11 rounded-full px-4 text-sm font-semibold tracking-[-0.01em] transition-colors disabled:opacity-40 ${
        pressed ? "bg-primary text-on-primary" : "border border-line bg-surface text-foreground"
      }`}
    >
      {children}
    </button>
  );
}
