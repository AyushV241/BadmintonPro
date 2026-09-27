"use client";

// MUI has no virtualization; its docs recommend react-window. The row content
// is the caller's (renderItem), so this wraps react-window only.

import { useMemo, useRef, type ReactNode } from "react";
import { List, type RowComponentProps } from "react-window";
import { testIdAttr } from "../../shared/dom";
import type { VirtualizedListProps } from "./VirtualizedList.types";

type RowData = {
  items: unknown[];
  renderItem: (item: unknown, index: number) => ReactNode;
};

// Defined outside the component so react-window sees a stable row type.
function Row({ index, style, ariaAttributes, items, renderItem }: RowComponentProps<RowData>) {
  return (
    <div style={style} {...ariaAttributes}>
      {renderItem(items[index], index)}
    </div>
  );
}

export function VirtualizedList<T>({
  items,
  renderItem,
  getKey,
  itemHeight,
  height = "100%",
  overscan = 4,
  onEndReached,
  endReachedThreshold = 5,
  emptyState,
  ariaLabel,
  className,
  id,
  testId,
}: VirtualizedListProps<T>) {
  const rowProps = useMemo(
    () => ({ items, renderItem }) as RowData,
    [items, renderItem],
  );
  // Fire onEndReached once per list length, not on every scroll event.
  const endReachedAt = useRef(-1);

  if (items.length === 0 && emptyState) {
    return (
      <div className={className} id={id} {...testIdAttr(testId)}>
        {emptyState}
      </div>
    );
  }

  return (
    <List
      rowComponent={Row}
      rowProps={rowProps}
      rowCount={items.length}
      rowHeight={
        typeof itemHeight === "number"
          ? itemHeight
          : (index: number) => itemHeight(items[index], index)
      }
      rowKey={getKey ? (index: number) => getKey(items[index], index) : undefined}
      overscanCount={overscan}
      onRowsRendered={({ stopIndex }) => {
        if (
          onEndReached &&
          stopIndex >= items.length - 1 - endReachedThreshold &&
          endReachedAt.current !== items.length
        ) {
          endReachedAt.current = items.length;
          onEndReached();
        }
      }}
      aria-label={ariaLabel}
      className={className}
      id={id}
      style={{ height }}
      {...testIdAttr(testId)}
    />
  );
}
