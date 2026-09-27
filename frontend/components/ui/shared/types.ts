// Shared vocabulary for every primitive's public contract.
// Nothing in this file may reference the underlying component library.

export type Size = "sm" | "md" | "lg";

export type Tone = "primary" | "neutral" | "success" | "warning" | "danger";

/** Props every primitive accepts. */
export interface BaseProps {
  /** Escape hatch for layout (margins, width, grid placement) via Tailwind. */
  className?: string;
  id?: string;
  /** Rendered as `data-testid` on the root element (on the `<input>` for text fields). */
  testId?: string;
}

export interface SelectOption<V extends string = string> {
  value: V;
  label: string;
  disabled?: boolean;
}
