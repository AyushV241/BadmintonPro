import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface RadioOption<V extends string = string> {
  value: V;
  label: ReactNode;
  disabled?: boolean;
}

/** A group of radio buttons; a single radio on its own is not a useful control. */
export interface RadioProps<V extends string = string> extends BaseProps {
  options: RadioOption<V>[];
  value?: V | null;
  defaultValue?: V;
  /** Receives the selected option's value. */
  onChange?: (value: V) => void;
  /** Group label, rendered as the legend. */
  label?: string;
  /** Required when there is no visible `label`. */
  ariaLabel?: string;
  helperText?: string;
  error?: string | boolean;
  name?: string;
  required?: boolean;
  disabled?: boolean;
  /** Default `vertical`. */
  direction?: "vertical" | "horizontal";
  /** Default `md`. */
  size?: "sm" | "md";
}
