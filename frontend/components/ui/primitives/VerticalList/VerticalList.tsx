"use client";

import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemIcon from "@mui/material/ListItemIcon";
import ListItemText from "@mui/material/ListItemText";
import ListSubheader from "@mui/material/ListSubheader";
import { testIdAttr } from "../../shared/dom";
import type { VerticalListItem, VerticalListProps } from "./VerticalList.types";

function Row({ item, divider }: { item: VerticalListItem; divider: boolean }) {
  const inner = (
    <>
      {item.leading && <ListItemIcon sx={{ minWidth: 40 }}>{item.leading}</ListItemIcon>}
      <ListItemText primary={item.primary} secondary={item.secondary} />
    </>
  );

  const interactive = Boolean(item.onClick || item.href);

  return (
    <ListItem
      divider={divider}
      disablePadding={interactive}
      secondaryAction={item.trailing}
      sx={interactive ? undefined : { opacity: item.disabled ? 0.5 : 1 }}
    >
      {interactive ? (
        item.href ? (
          <ListItemButton href={item.href} selected={item.selected} disabled={item.disabled}>
            {inner}
          </ListItemButton>
        ) : (
          <ListItemButton onClick={item.onClick} selected={item.selected} disabled={item.disabled}>
            {inner}
          </ListItemButton>
        )
      ) : (
        inner
      )}
    </ListItem>
  );
}

export function VerticalList({
  items,
  dividers = false,
  dense = false,
  subheader,
  emptyState,
  ariaLabel,
  className,
  id,
  testId,
}: VerticalListProps) {
  if (items.length === 0 && emptyState) {
    return (
      <div className={className} id={id} {...testIdAttr(testId)}>
        {emptyState}
      </div>
    );
  }

  return (
    <List
      dense={dense}
      subheader={subheader ? <ListSubheader component="div">{subheader}</ListSubheader> : undefined}
      aria-label={ariaLabel}
      className={className}
      id={id}
      {...testIdAttr(testId)}
    >
      {items.map((item, index) => (
        <Row key={item.id} item={item} divider={dividers && index < items.length - 1} />
      ))}
    </List>
  );
}
