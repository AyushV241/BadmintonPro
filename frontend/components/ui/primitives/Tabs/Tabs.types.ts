import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";
import type { TabListItem, TabListProps } from "../TabList/TabList.types";

export interface TabsItem<V extends string = string> extends TabListItem<V> {
  content: ReactNode;
}

export interface TabsProps<V extends string = string> extends BaseProps {
  items: TabsItem<V>[];
  value?: V;
  defaultValue?: V;
  onChange?: (value: V) => void;
  layout?: TabListProps<V>["layout"];
  /** Required: describes what the tabs switch between. */
  ariaLabel: string;
  /** Keep inactive panels mounted (preserves their state). Default `false`. */
  keepMounted?: boolean;
}
