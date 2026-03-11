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
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const onTimeUpdateRef = useRef(onTimeUpdate);
  const onPauseRef = useRef(onPause);
  const onEndedRef = useRef(onEnded);

  useEffect(() => {
    onTimeUpdateRef.current = onTimeUpdate;
  }, [onTimeUpdate]);

  useEffect(() => {
    onPauseRef.current = onPause;
  }, [onPause]);

  useEffect(() => {
    onEndedRef.current = onEnded;
  }, [onEnded]);

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
        // ArtPlayer mutates quality items by defining internal props; clone to avoid redefine errors on re-init.
        quality: sourceType === "m3u8" ? qualities.map((item) => ({ ...item })) : [],
        pip: true,
        mutex: true,
      });

      const video = artInstance.video as HTMLVideoElement;
      videoRef.current = video;

      const handleTimeUpdate = () => {
        onTimeUpdateRef.current?.(video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0);
      };
      const handlePause = () => {
        onPauseRef.current?.(video.currentTime || 0, Number.isFinite(video.duration) ? video.duration : 0);
      };
      const handleEnded = () => {
        onEndedRef.current?.(Number.isFinite(video.duration) ? video.duration : 0);
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
      videoRef.current = null;
      if (hlsInstance) {
        hlsInstance.destroy();
      }
      if (artInstance) {
        artInstance.destroy(false);
      }
    };
  }, [poster, qualities, sourceType, sourceUrl]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || startTimeSec <= 0) {
      return;
    }

    const seekToStart = () => {
      const duration = Number.isFinite(video.duration) ? video.duration : 0;
      if (duration > 0 && startTimeSec < duration - 1) {
        video.currentTime = startTimeSec;
      }
    };

    if (video.readyState >= 1) {
      seekToStart();
      return;
    }

    video.addEventListener("loadedmetadata", seekToStart, { once: true });
    return () => {
      video.removeEventListener("loadedmetadata", seekToStart);
    };
  }, [startTimeSec]);

  return (
    <div className="h-full w-full [&_.art-video-player]:!h-full [&_.art-video-player]:!w-full">
      <div ref={containerRef} className="h-full w-full" />
    </div>
  );
}
