"use client";

import MuiAccordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import Box from "@mui/material/Box";
import MuiTypography from "@mui/material/Typography";
import { useId, useState } from "react";
import { ChevronDownIcon } from "../../shared/icons";
import { testIdAttr } from "../../shared/dom";
import type { AccordionProps } from "./Accordion.types";

export function Accordion({
  items,
  multiple = false,
  expanded,
  defaultExpanded = [],
  onChange,
  variant = "outlined",
  className,
  id,
  testId,
}: AccordionProps) {
  const baseId = useId();
  const [uncontrolled, setUncontrolled] = useState(defaultExpanded);
  const open = expanded ?? uncontrolled;

  function toggle(itemId: string, isOpen: boolean) {
    const next = isOpen
      ? multiple
        ? [...open, itemId]
        : [itemId]
      : open.filter((openId) => openId !== itemId);
    if (expanded === undefined) setUncontrolled(next);
    onChange?.(next);
  }

  return (
    <Box className={className} id={id} {...testIdAttr(testId)}>
      {items.map((item) => (
        <MuiAccordion
          key={item.id}
          expanded={open.includes(item.id)}
          onChange={(_, isOpen) => toggle(item.id, isOpen)}
          disabled={item.disabled}
          disableGutters
          variant={variant === "outlined" ? "outlined" : "elevation"}
          elevation={0}
          sx={
            variant === "outlined"
              ? { mb: 1, borderRadius: 2, "&::before": { display: "none" } }
              : { bgcolor: "transparent" }
          }
        >
          <AccordionSummary
            expandIcon={<ChevronDownIcon />}
            id={`${baseId}-${item.id}-header`}
            aria-controls={`${baseId}-${item.id}-content`}
          >
            <Box>
              <MuiTypography component="span" variant="subtitle2" sx={{ display: "block" }}>
                {item.title}
              </MuiTypography>
              {item.subtitle && (
                <MuiTypography component="span" variant="body2" color="text.secondary">
                  {item.subtitle}
                </MuiTypography>
              )}
            </Box>
          </AccordionSummary>
          <AccordionDetails id={`${baseId}-${item.id}-content`}>{item.content}</AccordionDetails>
        </MuiAccordion>
      ))}
    </Box>
  );
}
