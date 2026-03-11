"use client";

import { useEffect, useRef } from "react";

type PlayerQuality = {
  html: string;
  url: string;
  default?: boolean;
};

type ArtHlsPlayerProps = {
  sourceUrl: string;
  sourceType: "m3u8" | "mp4";
  poster?: string;
  qualities?: PlayerQuality[];
  startTimeSec?: number;
  onTimeUpdate?: (positionSec: number, durationSec: number) => void;
  onPause?: (positionSec: number, durationSec: number) => void;
  onEnded?: (durationSec: number) => void;
};

export function ArtHlsPlayer({
  sourceUrl,
  sourceType,
  poster,
  qualities = [],
  startTimeSec = 0,
  onTimeUpdate,
  onPause,
  onEnded,
}: ArtHlsPlayerProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!containerRef.current || !sourceUrl) {
      return;
    }

    let disposed = false;
    let artInstance:
      | {
          destroy: (removeHtml?: boolean) => void;
          on?: (eventName: string, callback: () => void) => void;
          video?: HTMLVideoElement;
        }
      | null = null;
    let hlsInstance:
      | {
          destroy: () => void;
          loadSource: (url: string) => void;
          attachMedia: (video: HTMLMediaElement) => void;
        }
      | null = null;

    void (async () => {
      const [{ default: Artplayer }, { default: Hls }] = await Promise.all([import("artplayer"), import("hls.js")]);
      if (disposed || !containerRef.current) {
        return;
      }

      const customType =
        sourceType === "m3u8"
          ? {
              m3u8: (video: HTMLVideoElement, url: string) => {
                if (video.canPlayType("application/vnd.apple.mpegurl")) {
                  video.src = url;
                  return;
                }
                if (!Hls.isSupported()) {
                  video.src = url;
                  return;
                }
                if (hlsInstance) {
                  hlsInstance.destroy();
                }
                hlsInstance = new Hls();
                hlsInstance.loadSource(url);
                hlsInstance.attachMedia(video);
              },
            }
          : {};

      artInstance = new Artplayer({
        container: containerRef.current,
        url: sourceUrl,
        type: sourceType,
        customType,
        poster,
        volume: 0.7,
        autoplay: false,
        autoSize: true,
        fullscreen: true,
        fullscreenWeb: true,
        playbackRate: true,
        setting: true,
        quality: sourceType === "m3u8" ? qualities : [],
        pip: true,
        mutex: true,
      });

      const video = artInstance.video as HTMLVideoElement;
      let hasAppliedStartTime = false;
      const applyStartTime = () => {
        if (hasAppliedStartTime || startTimeSec <= 0) {
          return;
        }
        const duration = Number.isFinite(video.duration) ? video.duration : 0;
        if (duration > 0 && startTimeSec < duration - 1) {
          video.currentTime = startTimeSec;
        }
        hasAppliedStartTime = true;
      };
      if (video.readyState >= 1) {
        applyStartTime();
      } else {
        video.addEventListener("loadedmetadata", applyStartTime, { once: true });
      }

      const handleTimeUpdate = () => {
        onTimeUpdate?.(video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0);
      };
      const handlePause = () => {
        onPause?.(video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0);
      };
      const handleEnded = () => {
        onEnded?.(Number.isFinite(video.duration) ? video.duration : 0);
      };

      video.addEventListener("timeupdate", handleTimeUpdate);
      video.addEventListener("pause", handlePause);
      video.addEventListener("ended", handleEnded);

      const cleanupListeners = () => {
        video.removeEventListener("timeupdate", handleTimeUpdate);
        video.removeEventListener("pause", handlePause);
        video.removeEventListener("ended", handleEnded);
      };
      if (artInstance.on) {
        artInstance.on("destroy", cleanupListeners);
      }
    })();

    return () => {
      disposed = true;
      if (hlsInstance) {
        hlsInstance.destroy();
      }
      if (artInstance) {
        artInstance.destroy(false);
      }
    };
  }, [onEnded, onPause, onTimeUpdate, poster, qualities, sourceType, sourceUrl, startTimeSec]);

  return (
    <div className="h-full w-full [&_.art-video-player]:!h-full [&_.art-video-player]:!w-full">
      <div ref={containerRef} className="h-full w-full" />
    </div>
  );
}
