"use client";

// MUI has no video component; a <video> rendered through Box so it is themed
// (radius, background) like everything else.

import Box from "@mui/material/Box";
import { useEffect, useRef, type Ref } from "react";
import { testIdAttr } from "../../shared/dom";
import type { VideoRendererProps } from "./VideoRenderer.types";

function assignRef<T>(ref: Ref<T> | undefined, value: T | null) {
  if (typeof ref === "function") ref(value);
  else if (ref) ref.current = value;
}

export function VideoRenderer({
  src,
  stream,
  poster,
  controls,
  autoPlay,
  muted,
  loop,
  playsInline = true,
  aspectRatio = "16/9",
  fit = "contain",
  mirror,
  rounded = true,
  ariaLabel,
  onPlay,
  onPause,
  onEnded,
  onError,
  ref,
  className,
  id,
  testId,
}: VideoRendererProps) {
  const videoRef = useRef<HTMLVideoElement | null>(null);

  // `srcObject` has no attribute form; it must be set on the element.
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.srcObject = stream ?? null;
  }, [stream]);

  return (
    <Box
      component="video"
      ref={(el: HTMLVideoElement | null) => {
        videoRef.current = el;
        assignRef(ref, el);
      }}
      src={typeof src === "string" ? src : undefined}
      poster={poster}
      controls={controls ?? !stream}
      autoPlay={autoPlay}
      muted={muted}
      loop={loop}
      playsInline={playsInline}
      aria-label={ariaLabel}
      onPlay={onPlay}
      onPause={onPause}
      onEnded={onEnded}
      onError={onError}
      className={className}
      id={id}
      sx={{
        display: "block",
        width: "100%",
        aspectRatio: aspectRatio === "auto" ? undefined : aspectRatio.replace("/", " / "),
        objectFit: fit,
        bgcolor: "#000",
        borderRadius: rounded ? 2 : 0,
        transform: mirror ? "scaleX(-1)" : undefined,
      }}
      {...testIdAttr(testId)}
    >
      {Array.isArray(src) &&
        src.map((source) => <source key={source.src} src={source.src} type={source.type} />)}
    </Box>
  );
}
