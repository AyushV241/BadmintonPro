import type { BaseProps } from "../../shared/types";

/** A single number, or a `[min, max]` pair for a range slider. */
export type SliderValue = number | [number, number];

export interface SliderMark {
  value: number;
  label?: string;
}

export interface SliderProps<T extends SliderValue = number> extends BaseProps {
  value?: T;
  defaultValue?: T;
  /** Fires continuously while dragging. */
  onChange?: (value: T) => void;
  /** Fires once when the user releases the thumb. */
  onChangeEnd?: (value: T) => void;
  /** Default 0. */
  min?: number;
  /** Default 100. */
  max?: number;
  /** Default 1. */
  step?: number;
  /** `true` marks every step. */
  marks?: boolean | SliderMark[];
  /** Value bubble above the thumb. Default `auto` (while dragging/focused). */
  showValue?: "auto" | "always" | "never";
  formatValue?: (value: number) => string;
  /** Accessible name; required unless the slider is labelled elsewhere. */
  ariaLabel?: string;
  disabled?: boolean;
  /** Default `md`. */
  size?: "sm" | "md";
  name?: string;
}
