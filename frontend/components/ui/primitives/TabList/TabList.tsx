"use client";

import Tab from "@mui/material/Tab";
import MuiTabs from "@mui/material/Tabs";
import { useState } from "react";
import { testIdAttr } from "../../shared/dom";
import type { TabListProps } from "./TabList.types";

const LAYOUT_MAP = { fixed: "standard", fullWidth: "fullWidth", scrollable: "scrollable" } as const;

export function TabList<V extends string = string>({
  items,
  value,
  defaultValue,
  onChange,
  layout = "fixed",
  ariaLabel,
  idPrefix,
  className,
  id,
  testId,
}: TabListProps<V>) {
  const [uncontrolled, setUncontrolled] = useState<V | undefined>(defaultValue ?? items[0]?.value);
  const selected = value ?? uncontrolled;

  return (
    <MuiTabs
      value={selected ?? false}
      onChange={(_, next: V) => {
        if (value === undefined) setUncontrolled(next);
        onChange?.(next);
      }}
      variant={LAYOUT_MAP[layout]}
      scrollButtons={layout === "scrollable" ? "auto" : false}
      aria-label={ariaLabel}
      className={className}
      id={id}
      sx={{ borderBottom: 1, borderColor: "divider" }}
      {...testIdAttr(testId)}
    >
      {items.map((item) => (
        <Tab
          key={item.value}
          value={item.value}
          label={item.label}
          icon={item.icon ? <>{item.icon}</> : undefined}
          iconPosition="start"
          disabled={item.disabled}
          id={idPrefix ? `${idPrefix}-tab-${item.value}` : undefined}
          aria-controls={idPrefix ? `${idPrefix}-panel-${item.value}` : undefined}
          sx={{ minHeight: 48 }}
        />
      ))}
    </MuiTabs>
  );
}
