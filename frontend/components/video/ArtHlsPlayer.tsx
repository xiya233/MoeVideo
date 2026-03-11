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
};

export function ArtHlsPlayer({ sourceUrl, sourceType, poster, qualities = [] }: ArtHlsPlayerProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!containerRef.current || !sourceUrl) {
      return;
    }

    let disposed = false;
    let artInstance: { destroy: (removeHtml?: boolean) => void } | null = null;
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
  }, [poster, qualities, sourceType, sourceUrl]);

  return (
    <div className="h-full w-full [&_.art-video-player]:!h-full [&_.art-video-player]:!w-full">
      <div ref={containerRef} className="h-full w-full" />
    </div>
  );
}
