import type { Key, ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

/** A single horizontally scrolling row of arbitrary items (chips, cards, avatars). */
export interface HorizontalListProps<T> extends BaseProps {
  items: T[];
  renderItem: (item: T, index: number) => ReactNode;
  getKey: (item: T, index: number) => Key;
  /** Space between items. Default `md`. */
  gap?: "none" | "sm" | "md" | "lg";
  /** Fixed width for every item, e.g. `160` or `"70%"`. Omit to size by content. */
  itemWidth?: number | string;
  /** Snap each item into place when scrolling stops. Default `false`. */
  snap?: boolean;
  /** Default `false`. */
  showScrollbar?: boolean;
  /** Shown when `items` is empty. */
  emptyState?: ReactNode;
  ariaLabel?: string;
}
