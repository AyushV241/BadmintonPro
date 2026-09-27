import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface CheckboxProps extends BaseProps {
  checked?: boolean;
  defaultChecked?: boolean;
  /** Shows a dash instead of a tick, e.g. for "some selected". */
  indeterminate?: boolean;
  /** Receives the new checked state, not a DOM event. */
  onChange?: (checked: boolean) => void;
  label?: ReactNode;
  /** Required when there is no visible `label`. */
  ariaLabel?: string;
  name?: string;
  value?: string;
  required?: boolean;
  disabled?: boolean;
  /** Default `md`. */
  size?: "sm" | "md";
}
