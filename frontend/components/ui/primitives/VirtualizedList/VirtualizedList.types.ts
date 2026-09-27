import type { Key, ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

/** A long vertical list that only renders the rows on screen. */
export interface VirtualizedListProps<T> extends BaseProps {
  items: T[];
  renderItem: (item: T, index: number) => ReactNode;
  getKey?: (item: T, index: number) => Key;
  /** Row height in px: one number for all rows, or per row. */
  itemHeight: number | ((item: T, index: number) => number);
  /** Height of the scroll area, e.g. `480` or `"60vh"`. Default `100%` of the parent. */
  height?: number | string;
  /** Extra rows rendered above/below the viewport. Default 4. */
  overscan?: number;
  /** For infinite scroll: fires once when rows near the end become visible. */
  onEndReached?: () => void;
  /** How many rows before the end to fire `onEndReached`. Default 5. */
  endReachedThreshold?: number;
  /** Shown when `items` is empty. */
  emptyState?: ReactNode;
  ariaLabel?: string;
}
