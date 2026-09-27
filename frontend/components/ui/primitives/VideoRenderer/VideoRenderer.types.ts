import type { Ref } from "react";
import type { BaseProps } from "../../shared/types";

export interface VideoSource {
  src: string;
  /** MIME type, e.g. `video/mp4`. */
  type?: string;
}

/**
 * Renders a video file (`src`) or a live MediaStream (`stream`, e.g. a camera
 * or WebRTC track). Pass exactly one.
 */
export interface VideoRendererProps extends BaseProps {
  /** A URL, or several sources for the browser to choose from. */
  src?: string | VideoSource[];
  /** A live stream. Autoplay of a stream generally requires `muted`. */
  stream?: MediaStream | null;
  poster?: string;
  /** Native playback controls. Default `true` for `src`, `false` for `stream`. */
  controls?: boolean;
  autoPlay?: boolean;
  muted?: boolean;
  loop?: boolean;
  /** Default `true` (stops iOS forcing fullscreen). */
  playsInline?: boolean;
  /** Box shape; `auto` uses the video's own. Default `16/9`. */
  aspectRatio?: "16/9" | "4/3" | "1/1" | "9/16" | "auto";
  /** Default `contain`. */
  fit?: "contain" | "cover";
  /** Flip horizontally, e.g. for a front-camera preview. */
  mirror?: boolean;
  /** Default `true`. */
  rounded?: boolean;
  ariaLabel?: string;
  onPlay?: () => void;
  onPause?: () => void;
  onEnded?: () => void;
  onError?: () => void;
  /** Access the underlying <video> for play()/pause()/currentTime. */
  ref?: Ref<HTMLVideoElement>;
}
