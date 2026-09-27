import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface CardMedia {
  src: string;
  alt: string;
  /** Pixel height of the image area. Default 160. */
  height?: number;
}

export interface CardProps extends BaseProps {
  children?: ReactNode;
  title?: ReactNode;
  subtitle?: ReactNode;
  /** Top-right of the header, e.g. a menu button. */
  headerAction?: ReactNode;
  /** Image shown above the header. */
  media?: CardMedia;
  /** Footer row, e.g. buttons. */
  actions?: ReactNode;
  /** Default `outlined`. */
  variant?: "outlined" | "elevated";
  /** Inner spacing of header/body/footer. Default `md`. */
  padding?: "none" | "sm" | "md";
  /**
   * Makes the whole card (media, header, body) one clickable surface.
   * Don't put other buttons in `children` when using this; use `actions`.
   */
  onClick?: () => void;
  /** Like `onClick`, but navigates. */
  href?: string;
}
