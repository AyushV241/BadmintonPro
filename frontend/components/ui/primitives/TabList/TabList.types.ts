import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface TabListItem<V extends string = string> {
  value: V;
  label: ReactNode;
  icon?: ReactNode;
  disabled?: boolean;
}

/**
 * The row of tabs on its own. Use this when the selected tab drives something
 * other than an adjacent panel (a route, a filter). For tabs with panels use `Tabs`.
 */
export interface TabListProps<V extends string = string> extends BaseProps {
  items: TabListItem<V>[];
  value?: V;
  defaultValue?: V;
  onChange?: (value: V) => void;
  /**
   * `fixed`: natural width, left-aligned. `fullWidth`: tabs share the width.
   * `scrollable`: overflow scrolls horizontally. Default `fixed`.
   */
  layout?: "fixed" | "fullWidth" | "scrollable";
  /** Required: describes what the tabs switch between. */
  ariaLabel: string;
  /**
   * When set, each tab gets `id="{idPrefix}-tab-{value}"` and
   * `aria-controls="{idPrefix}-panel-{value}"`. `Tabs` uses this to wire panels.
   */
  idPrefix?: string;
}
