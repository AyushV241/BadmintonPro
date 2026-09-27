import type { BaseProps } from "../../shared/types";

export interface OTPInputProps extends BaseProps {
  /** Number of characters. Default 6. */
  length?: number;
  value?: string;
  defaultValue?: string;
  /** Receives the code typed so far (may be shorter than `length`). */
  onChange?: (value: string) => void;
  /** Fires once every box is filled. */
  onComplete?: (value: string) => void;
  /** Allowed characters. Default `numeric`. */
  type?: "numeric" | "alphanumeric";
  /** Hide characters like a password. Default `false`. */
  mask?: boolean;
  /** A string shows as the message under the boxes; `true` only marks them invalid. */
  error?: string | boolean;
  disabled?: boolean;
  autoFocus?: boolean;
  /** Default "Verification code". Each box is announced as "{ariaLabel}, character 2 of 6". */
  ariaLabel?: string;
  name?: string;
}
