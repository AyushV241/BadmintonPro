"use client";

import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import { testIdAttr } from "../../shared/dom";
import type { HorizontalListProps } from "./HorizontalList.types";

// MUI spacing units (8px).
const GAP = { none: 0, sm: 1, md: 2, lg: 3 } as const;

export function HorizontalList<T>({
  items,
  renderItem,
  getKey,
  gap = "md",
  itemWidth,
  snap = false,
  showScrollbar = false,
  emptyState,
  ariaLabel,
  className,
  id,
  testId,
}: HorizontalListProps<T>) {
  if (items.length === 0 && emptyState) {
    return (
      <div className={className} id={id} {...testIdAttr(testId)}>
        {emptyState}
      </div>
    );
  }

  return (
    <Stack
      component="ul"
      direction="row"
      spacing={GAP[gap]}
      aria-label={ariaLabel}
      className={className}
      id={id}
      sx={{
        listStyle: "none",
        m: 0,
        p: 0,
        overflowX: "auto",
        overscrollBehaviorX: "contain",
        scrollSnapType: snap ? "x mandatory" : undefined,
        ...(showScrollbar
          ? {}
          : { scrollbarWidth: "none", "&::-webkit-scrollbar": { display: "none" } }),
      }}
      {...testIdAttr(testId)}
    >
      {items.map((item, index) => (
        <Box
          component="li"
          key={getKey(item, index)}
          sx={{
            flexShrink: 0,
            width: itemWidth,
            scrollSnapAlign: snap ? "start" : undefined,
          }}
        >
          {renderItem(item, index)}
        </Box>
      ))}
    </Stack>
  );
}
