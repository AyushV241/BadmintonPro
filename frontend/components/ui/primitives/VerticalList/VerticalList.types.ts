import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface VerticalListItem {
  /** Stable React key. */
  id: string;
  primary: ReactNode;
  secondary?: ReactNode;
  /** Left slot: avatar, icon, checkbox. */
  leading?: ReactNode;
  /** Right slot: badge, chevron, action button. */
  trailing?: ReactNode;
  /** Makes the row clickable. */
  onClick?: () => void;
  /** Makes the row a link. */
  href?: string;
  selected?: boolean;
  disabled?: boolean;
}

export interface VerticalListProps extends BaseProps {
  items: VerticalListItem[];
  /** Draw a line between rows. Default `false`. */
  dividers?: boolean;
  /** Tighter row height. Default `false`. */
  dense?: boolean;
  /** Heading above the list. */
  subheader?: ReactNode;
  /** Shown when `items` is empty. */
  emptyState?: ReactNode;
  /** Required when there is no `subheader`. */
  ariaLabel?: string;
}
