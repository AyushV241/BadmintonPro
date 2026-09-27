"use client";

// MUI has no carousel. This is a CSS scroll-snap track (native touch swiping,
// no JS drag handling) with MUI IconButtons for arrows.

import Box from "@mui/material/Box";
import ButtonBase from "@mui/material/ButtonBase";
import IconButton from "@mui/material/IconButton";
import { useEffect, useRef, useState } from "react";
import { ChevronLeftIcon, ChevronRightIcon } from "../../shared/icons";
import { testIdAttr } from "../../shared/dom";
import type { CarouselProps } from "./Carousel.types";

const GAP_PX = { none: 0, sm: 8, md: 16 } as const;

export function Carousel<T>({
  items,
  renderItem,
  getKey,
  slidesPerView = 1,
  gap = "md",
  index,
  defaultIndex = 0,
  onIndexChange,
  showDots = true,
  showArrows = true,
  ariaLabel,
  className,
  id,
  testId,
}: CarouselProps<T>) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [uncontrolled, setUncontrolled] = useState(defaultIndex);
  const active = index ?? uncontrolled;
  // Last index reported by scrolling, to avoid re-scrolling to it.
  const scrolledIndex = useRef(active);

  const gapPx = GAP_PX[gap];
  const lastIndex = Math.max(0, items.length - slidesPerView);

  function scrollToIndex(target: number, behavior: ScrollBehavior = "smooth") {
    const track = trackRef.current;
    const slide = track?.children[target] as HTMLElement | undefined;
    if (!track || !slide) return;
    track.scrollTo({ left: slide.offsetLeft - track.offsetLeft, behavior });
  }

  function select(next: number) {
    const clamped = Math.min(lastIndex, Math.max(0, next));
    scrolledIndex.current = clamped;
    if (index === undefined) setUncontrolled(clamped);
    onIndexChange?.(clamped);
    scrollToIndex(clamped);
  }

  // Controlled `index` changed from outside: follow it.
  useEffect(() => {
    if (active !== scrolledIndex.current) {
      scrolledIndex.current = active;
      scrollToIndex(active);
    }
  }, [active]);

  // Start at defaultIndex without animating.
  useEffect(() => {
    if (active > 0) scrollToIndex(active, "instant");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function handleScroll() {
    const track = trackRef.current;
    const first = track?.children[0] as HTMLElement | undefined;
    if (!track || !first) return;
    const next = Math.min(lastIndex, Math.round(track.scrollLeft / (first.offsetWidth + gapPx)));
    if (next !== scrolledIndex.current) {
      scrolledIndex.current = next;
      if (index === undefined) setUncontrolled(next);
      onIndexChange?.(next);
    }
  }

  const slideWidth = `calc((100% - ${gapPx * (slidesPerView - 1)}px) / ${slidesPerView})`;
  const arrowSx = {
    position: "absolute",
    top: "50%",
    transform: "translateY(-50%)",
    bgcolor: "background.paper",
    boxShadow: 1,
    "&:hover": { bgcolor: "background.paper" },
    "&.Mui-disabled": { opacity: 0 },
  } as const;

  return (
    <Box
      component="section"
      aria-roledescription="carousel"
      aria-label={ariaLabel}
      className={className}
      id={id}
      {...testIdAttr(testId)}
    >
      <Box sx={{ position: "relative" }}>
        <Box
          ref={trackRef}
          onScroll={handleScroll}
          sx={{
            display: "flex",
            gap: `${gapPx}px`,
            overflowX: "auto",
            scrollSnapType: "x mandatory",
            overscrollBehaviorX: "contain",
            scrollbarWidth: "none",
            "&::-webkit-scrollbar": { display: "none" },
          }}
        >
          {items.map((item, i) => (
            <Box
              key={getKey(item, i)}
              role="group"
              aria-roledescription="slide"
              aria-label={`${i + 1} of ${items.length}`}
              sx={{ flex: `0 0 ${slideWidth}`, scrollSnapAlign: "start", minWidth: 0 }}
            >
              {renderItem(item, i)}
            </Box>
          ))}
        </Box>

        {showArrows && items.length > slidesPerView && (
          <>
            <IconButton
              aria-label="Previous slide"
              onClick={() => select(active - 1)}
              disabled={active <= 0}
              size="small"
              sx={{ ...arrowSx, left: 8 }}
            >
              <ChevronLeftIcon />
            </IconButton>
            <IconButton
              aria-label="Next slide"
              onClick={() => select(active + 1)}
              disabled={active >= lastIndex}
              size="small"
              sx={{ ...arrowSx, right: 8 }}
            >
              <ChevronRightIcon />
            </IconButton>
          </>
        )}
      </Box>

      {showDots && lastIndex > 0 && (
        <Box sx={{ display: "flex", justifyContent: "center", gap: 0.5, mt: 1.5 }}>
          {Array.from({ length: lastIndex + 1 }, (_, i) => (
            <ButtonBase
              key={i}
              aria-label={`Go to slide ${i + 1}`}
              aria-current={i === active ? "true" : undefined}
              onClick={() => select(i)}
              sx={{ p: 0.75, borderRadius: "50%" }}
            >
              <Box
                sx={{
                  width: i === active ? 18 : 6,
                  height: 6,
                  borderRadius: 3,
                  bgcolor: i === active ? "primary.main" : "divider",
                  transition: "width 200ms, background-color 200ms",
                }}
              />
            </ButtonBase>
          ))}
        </Box>
      )}
    </Box>
  );
}
