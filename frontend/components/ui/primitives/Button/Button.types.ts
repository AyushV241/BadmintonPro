import type { MouseEvent, ReactNode, Ref } from "react";
import type { BaseProps, Size } from "../../shared/types";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

export interface ButtonProps extends BaseProps {
  children: ReactNode;
  /** Default `primary`. */
  variant?: ButtonVariant;
  /** Default `md`. */
  size?: Size;
  type?: "button" | "submit" | "reset";
  disabled?: boolean;
  /** Shows a spinner and disables the button. */
  loading?: boolean;
  fullWidth?: boolean;
  startIcon?: ReactNode;
  endIcon?: ReactNode;
  /** Renders the button as a link. */
  href?: string;
  onClick?: (event: MouseEvent<HTMLElement>) => void;
  /** Required when `children` is not readable text (e.g. an icon). */
  ariaLabel?: string;
  ref?: Ref<HTMLButtonElement>;
}
