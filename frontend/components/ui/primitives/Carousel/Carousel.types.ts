import type { Key, ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface CarouselProps<T> extends BaseProps {
  items: T[];
  renderItem: (item: T, index: number) => ReactNode;
  getKey: (item: T, index: number) => Key;
  /** Slides visible at once. Default 1. */
  slidesPerView?: number;
  /** Space between slides. Default `md`. */
  gap?: "none" | "sm" | "md";
  /** Controlled index of the first visible slide. */
  index?: number;
  defaultIndex?: number;
  /** Fires when the user swipes/scrolls or uses the arrows/dots. */
  onIndexChange?: (index: number) => void;
  /** Default `true`. */
  showDots?: boolean;
  /** Previous/next buttons. Default `true`. */
  showArrows?: boolean;
  /** Required: what the carousel shows, e.g. "Match highlights". */
  ariaLabel: string;
}
