"use client";

import Box from "@mui/material/Box";
import IconButton from "@mui/material/IconButton";
import SwipeableDrawer from "@mui/material/SwipeableDrawer";
import MuiTypography from "@mui/material/Typography";
import { useId } from "react";
import { testIdAttr } from "../../shared/dom";
import { CloseIcon } from "../../shared/icons";
import type { BottomSheetProps, BottomSheetSize } from "./BottomSheet.types";
import { useBottomSheet } from "./useBottomSheet";

const HEIGHT: Record<BottomSheetSize, string | undefined> = {
  auto: undefined,
  half: "50dvh",
  full: "100dvh",
};

// MUI's recommendation: iOS has its own swipe-back gesture and a slow backdrop
// transition, so tune the drawer for it.
const isIOS = typeof navigator !== "undefined" && /iPad|iPhone|iPod/.test(navigator.userAgent);

export function BottomSheet({
  hash,
  title,
  description,
  children,
  footer,
  size = "auto",
  dismissible = true,
  showCloseButton = true,
  ariaLabel,
  className,
  id,
  testId,
}: BottomSheetProps) {
  const { isOpen, close } = useBottomSheet(hash);
  const titleId = useId();
  const descriptionId = useId();
  const hasHeader = Boolean(title || description || showCloseButton);

  return (
    <SwipeableDrawer
      anchor="bottom"
      open={isOpen}
      onClose={() => {
        if (dismissible) close();
      }}
      // Opening is always programmatic; there is no edge to swipe up from.
      onOpen={() => {}}
      disableSwipeToOpen
      // SwipeableDrawer keeps closed sheets mounted so swipe-to-open can find
      // the paper; we don't swipe to open, so unmount when closed.
      ModalProps={{ keepMounted: false }}
      disableDiscovery={isIOS}
      disableBackdropTransition={!isIOS}
      id={id}
      slotProps={{
        paper: {
          className,
          role: "dialog",
          "aria-modal": true,
          "aria-labelledby": title ? titleId : undefined,
          "aria-describedby": description ? descriptionId : undefined,
          "aria-label": title ? undefined : ariaLabel,
          ...testIdAttr(testId),
          sx: {
            height: HEIGHT[size],
            maxHeight: size === "full" ? "100dvh" : "90dvh",
            borderTopLeftRadius: size === "full" ? 0 : 20,
            borderTopRightRadius: size === "full" ? 0 : 20,
            display: "flex",
            flexDirection: "column",
            overflow: "hidden",
          },
        },
      }}
    >
      <Box
        aria-hidden
        sx={{ width: 40, height: 5, borderRadius: 3, bgcolor: "divider", mx: "auto", mt: 1.25, flexShrink: 0 }}
      />

      {hasHeader && (
        <Box sx={{ display: "flex", alignItems: "flex-start", gap: 1, px: 2.5, pt: 1.5, pb: 1, flexShrink: 0 }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            {title && (
              <MuiTypography id={titleId} variant="h6" component="h2">
                {title}
              </MuiTypography>
            )}
            {description && (
              <MuiTypography id={descriptionId} variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                {description}
              </MuiTypography>
            )}
          </Box>
          {showCloseButton && (
            <IconButton aria-label="Close" onClick={close} size="small" sx={{ mr: -1 }}>
              <CloseIcon />
            </IconButton>
          )}
        </Box>
      )}

      <Box sx={{ flex: 1, overflowY: "auto", overscrollBehavior: "contain", px: 2.5, py: 1.5 }}>
        {children}
      </Box>

      {footer && (
        <Box
          sx={{
            flexShrink: 0,
            px: 2.5,
            pt: 2,
            pb: "calc(16px + env(safe-area-inset-bottom))",
            borderTop: 1,
            borderColor: "divider",
          }}
        >
          {footer}
        </Box>
      )}
    </SwipeableDrawer>
  );
}
