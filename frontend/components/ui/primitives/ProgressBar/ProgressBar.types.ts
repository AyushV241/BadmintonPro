import type { BaseProps, Size } from "../../shared/types";

export interface ProgressBarProps extends BaseProps {
  /** 0–100. Omit for an indeterminate (looping) bar. */
  value?: number;
  /** Default `primary`. */
  tone?: "primary" | "success" | "warning" | "danger";
  /** Bar thickness. Default `md`. */
  size?: Size;
  /** Shows the rounded percentage next to the bar. Ignored when indeterminate. */
  showValue?: boolean;
  /** Accessible name, e.g. "Upload progress". */
  ariaLabel?: string;
}
