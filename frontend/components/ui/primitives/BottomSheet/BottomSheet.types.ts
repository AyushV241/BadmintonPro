import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export type BottomSheetSize = "auto" | "half" | "full";

/**
 * A bottom sheet whose open state lives in the URL hash: open it with
 * `useBottomSheet(hash).open()`, and Back closes it. Swipe down to dismiss,
 * header with a close button, scrollable body, sticky footer.
 *
 * @example
 * const filters = useBottomSheet("filters");
 * <Button onClick={filters.open}>Filters</Button>
 * <BottomSheet hash="filters" title="Filters">…</BottomSheet>
 */
export interface BottomSheetProps extends BaseProps {
  /** Hash without `#`, unique per page, e.g. `"filters"`. */
  hash: string;
  title?: ReactNode;
  /** Line under the title. */
  description?: ReactNode;
  children: ReactNode;
  /** Sticky at the bottom, e.g. a primary action. */
  footer?: ReactNode;
  /** `auto` fits content (max 90% of the screen). Default `auto`. */
  size?: BottomSheetSize;
  /** Allow closing by swipe, backdrop and Escape. Back always closes it. Default `true`. */
  dismissible?: boolean;
  /** Close (×) button in the header. Default `true`. */
  showCloseButton?: boolean;
  /** Required when there is no `title`. */
  ariaLabel?: string;
}
