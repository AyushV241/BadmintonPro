import type { Ref } from "react";
import type { BaseProps } from "../../shared/types";

export interface TextareaProps extends BaseProps {
  value?: string;
  defaultValue?: string;
  onChange?: (value: string) => void;
  onBlur?: () => void;
  label?: string;
  placeholder?: string;
  helperText?: string;
  /** A string shows as the message under the field; `true` only marks it invalid. */
  error?: string | boolean;
  name?: string;
  required?: boolean;
  disabled?: boolean;
  readOnly?: boolean;
  /** Fixed number of rows. Overrides `minRows`/`maxRows`. */
  rows?: number;
  /** Grows with content from this many rows. Default 3. */
  minRows?: number;
  maxRows?: number;
  maxLength?: number;
  /** Shows a `12 / 280` counter. Needs `maxLength`. */
  showCount?: boolean;
  /** Default `true`. */
  fullWidth?: boolean;
  ref?: Ref<HTMLTextAreaElement>;
}
