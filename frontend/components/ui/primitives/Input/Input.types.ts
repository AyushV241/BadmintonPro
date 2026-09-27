import type { HTMLAttributes, ReactNode, Ref } from "react";
import type { BaseProps } from "../../shared/types";

export type InputType = "text" | "email" | "password" | "number" | "tel" | "url" | "search";

export interface InputProps extends BaseProps {
  /** Controlled value. Omit and use `defaultValue` for uncontrolled. */
  value?: string;
  defaultValue?: string;
  /** Receives the new string value, not a DOM event. */
  onChange?: (value: string) => void;
  onBlur?: () => void;
  onFocus?: () => void;
  label?: string;
  placeholder?: string;
  helperText?: string;
  /** A string shows as the message under the field; `true` only marks it invalid. */
  error?: string | boolean;
  /** Default `text`. */
  type?: InputType;
  name?: string;
  required?: boolean;
  disabled?: boolean;
  readOnly?: boolean;
  autoComplete?: string;
  autoFocus?: boolean;
  inputMode?: HTMLAttributes<HTMLInputElement>["inputMode"];
  maxLength?: number;
  /** Default `md`. */
  size?: "sm" | "md";
  /** Default `true`. */
  fullWidth?: boolean;
  startAdornment?: ReactNode;
  endAdornment?: ReactNode;
  ref?: Ref<HTMLInputElement>;
}
