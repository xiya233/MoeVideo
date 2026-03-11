"use client";

import { useEffect, useRef } from "react";

type ArtHlsPlayerProps = {
  sourceUrl: string;
  sourceType: "m3u8" | "mp4";
  poster?: string;
};

export function ArtHlsPlayer({ sourceUrl, sourceType, poster }: ArtHlsPlayerProps) {
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
  }, [poster, sourceType, sourceUrl]);

  return (
    <div className="h-full w-full [&_.art-video-player]:!h-full [&_.art-video-player]:!w-full">
      <div ref={containerRef} className="h-full w-full" />
    </div>
  );
}
