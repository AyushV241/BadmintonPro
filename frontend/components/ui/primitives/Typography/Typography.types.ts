import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export type TypographyVariant =
  | "display"
  | "h1"
  | "h2"
  | "h3"
  | "title"
  | "body"
  | "bodySmall"
  | "label"
  | "caption"
  | "overline";

export type TypographyColor = "default" | "muted" | "primary" | "success" | "danger" | "inherit";

export type TypographyElement = "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "p" | "span" | "div" | "label";

export interface TypographyProps extends BaseProps {
  children: ReactNode;
  /** Visual style. Default `body`. */
  variant?: TypographyVariant;
  /** HTML element to render. Defaults to a sensible element for the variant. */
  as?: TypographyElement;
  color?: TypographyColor;
  align?: "left" | "center" | "right";
  weight?: "regular" | "medium" | "semibold" | "bold";
  /** `true` truncates to one line with an ellipsis; a number clamps to that many lines. */
  truncate?: boolean | number;
  /** For `as="label"`. */
  htmlFor?: string;
}
