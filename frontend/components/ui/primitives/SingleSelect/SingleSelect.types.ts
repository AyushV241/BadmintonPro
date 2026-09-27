import type { BaseProps, SelectOption } from "../../shared/types";

export interface SingleSelectProps<V extends string = string> extends BaseProps {
  options: SelectOption<V>[];
  /** Controlled value; `null` means nothing selected. */
  value?: V | null;
  defaultValue?: V;
  /** Receives the selected option's value. */
  onChange?: (value: V) => void;
  label?: string;
  /** Shown when nothing is selected. */
  placeholder?: string;
  helperText?: string;
  error?: string | boolean;
  name?: string;
  required?: boolean;
  disabled?: boolean;
  /** Default `md`. */
  size?: "sm" | "md";
  /** Default `true`. */
  fullWidth?: boolean;
}
