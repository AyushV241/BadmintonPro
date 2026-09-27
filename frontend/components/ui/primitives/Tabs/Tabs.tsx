"use client";

// Composite: built from our own TabList, not from MUI directly, so it does not
// need to change when TabList's implementation does.

import { useId, useState } from "react";
import { testIdAttr } from "../../shared/dom";
import { TabList } from "../TabList/TabList";
import type { TabsProps } from "./Tabs.types";

export function Tabs<V extends string = string>({
  items,
  value,
  defaultValue,
  onChange,
  layout,
  ariaLabel,
  keepMounted = false,
  className,
  id,
  testId,
}: TabsProps<V>) {
  const idPrefix = useId();
  const [uncontrolled, setUncontrolled] = useState<V | undefined>(defaultValue ?? items[0]?.value);
  const selected = value ?? uncontrolled;

  return (
    <div className={className} id={id} {...testIdAttr(testId)}>
      <TabList
        items={items}
        value={selected}
        onChange={(next) => {
          if (value === undefined) setUncontrolled(next);
          onChange?.(next);
        }}
        layout={layout}
        ariaLabel={ariaLabel}
        idPrefix={idPrefix}
      />
      {items.map((item) => {
        const active = item.value === selected;
        if (!active && !keepMounted) return null;
        return (
          <div
            key={item.value}
            role="tabpanel"
            id={`${idPrefix}-panel-${item.value}`}
            aria-labelledby={`${idPrefix}-tab-${item.value}`}
            hidden={!active}
            style={{ paddingTop: 16 }}
          >
            {item.content}
          </div>
        );
      })}
    </div>
  );
}
