"use client";

import { useEffect, useMemo, useRef } from "react";
import type { Danmu, Result as DanmakuPluginResult } from "artplayer-plugin-danmuku";

import type { DanmakuItem } from "@/lib/dto";

const DEFAULT_PLAYER_THEME = "#3db8f5";
const REALTIME_DANMAKU_OFFSET_SEC = 0.15;

type PlayerQuality = {
  html: string;
  url: string;
  default?: boolean;
};

type EmitDanmakuPayload = {
  content: string;
  time_sec: number;
  mode: 0 | 1 | 2;
  color: string;
};

type ArtHlsPlayerProps = {
  sourceUrl: string;
  sourceType: "m3u8" | "mp4";
  poster?: string;
  vttThumbnailUrl?: string;
  qualities?: PlayerQuality[];
  qualitySignature?: string;
  startTimeSec?: number;
  danmakuItems?: DanmakuItem[];
  latestDanmaku?: DanmakuItem | null;
  canEmitDanmaku?: boolean;
  onEmitDanmaku?: (payload: EmitDanmakuPayload) => Promise<boolean>;
  onTimeUpdate?: (positionSec: number, durationSec: number) => void;
  onPause?: (positionSec: number, durationSec: number) => void;
  onEnded?: (durationSec: number) => void;
};

type ArtplayerInstance = {
  destroy: (removeHtml?: boolean) => void;
  switchQuality?: (url: string) => Promise<void>;
  on?: (eventName: string, callback: () => void) => void;
  video?: HTMLVideoElement;
  plugins?: {
    artplayerPluginDanmuku?: DanmakuPluginResult;
  };
};

function resolvePlayerThemeColor(): string {
  if (typeof window === "undefined") {
    return DEFAULT_PLAYER_THEME;
  }
  const computed = window.getComputedStyle(document.documentElement);
  const color = computed.getPropertyValue("--color-primary").trim();
  return color || DEFAULT_PLAYER_THEME;
}

function toPluginDanmu(item: DanmakuItem): Danmu {
  return {
    text: item.content,
    time: Math.max(0, item.time_sec || 0),
    mode: item.mode,
    color: item.color || "#FFFFFF",
  };
}

function toRealtimeDanmu(item: DanmakuItem, currentTimeSec: number): Danmu {
  return {
    text: item.content,
    time: Math.max(0, item.time_sec || 0, Math.max(0, currentTimeSec) + REALTIME_DANMAKU_OFFSET_SEC),
    mode: item.mode,
    color: item.color || "#FFFFFF",
  };
}

function normalizeMode(raw: unknown): 0 | 1 | 2 {
  if (raw === 1 || raw === 2) {
    return raw;
  }
  return 0;
}

function normalizeColor(raw: unknown): string {
  if (typeof raw !== "string") {
    return "#FFFFFF";
  }
  const color = raw.trim();
  return color || "#FFFFFF";
}

function qualityLabel(html: string | HTMLElement): string {
  if (typeof html === "string") {
    return html;
  }
  return html.textContent?.trim() || "";
}

export function ArtHlsPlayer({
  sourceUrl,
  sourceType,
  poster,
  vttThumbnailUrl,
  qualities = [],
  qualitySignature,
  startTimeSec = 0,
  danmakuItems = [],
  latestDanmaku,
  canEmitDanmaku = false,
  onEmitDanmaku,
  onTimeUpdate,
  onPause,
  onEnded,
}: ArtHlsPlayerProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const danmakuPluginRef = useRef<DanmakuPluginResult | null>(null);
  const loadedDanmakuSignatureRef = useRef("");
  const latestDanmakuIDRef = useRef("");
  const pendingLatestDanmakuRef = useRef<DanmakuItem | null>(null);
  const danmakuItemsRef = useRef(danmakuItems);
  const canEmitDanmakuRef = useRef(canEmitDanmaku);
  const onTimeUpdateRef = useRef(onTimeUpdate);
  const onPauseRef = useRef(onPause);
  const onEndedRef = useRef(onEnded);
  const onEmitDanmakuRef = useRef(onEmitDanmaku);

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
    onEmitDanmakuRef.current = onEmitDanmaku;
  }, [onEmitDanmaku]);

  useEffect(() => {
    danmakuItemsRef.current = danmakuItems;
  }, [danmakuItems]);

  useEffect(() => {
    canEmitDanmakuRef.current = canEmitDanmaku;
  }, [canEmitDanmaku]);

  const resolvedQualitySignature = useMemo(() => {
    if (typeof qualitySignature === "string") {
      return qualitySignature;
    }
    if (qualities.length === 0) {
      return "";
    }
    return qualities.map((item) => `${item.html}|${item.url}|${item.default ? "1" : "0"}`).join("||");
  }, [qualities, qualitySignature]);

  const danmakuSignature = useMemo(() => {
    if (danmakuItems.length === 0) {
      return "";
    }
    return danmakuItems.map((item) => item.id).join("|");
  }, [danmakuItems]);

  useEffect(() => {
    loadedDanmakuSignatureRef.current = "";
    latestDanmakuIDRef.current = "";
    pendingLatestDanmakuRef.current = null;
  }, [sourceUrl]);

  useEffect(() => {
    if (!containerRef.current || !sourceUrl) {
      return;
    }

    let disposed = false;
    let artInstance: ArtplayerInstance | null = null;
    let hlsInstance:
      | {
          destroy: () => void;
          loadSource: (url: string) => void;
          attachMedia: (video: HTMLMediaElement) => void;
        }
      | null = null;

    void (async () => {
      const [
        { default: Artplayer },
        { default: Hls },
        { default: artplayerPluginDanmuku },
        { default: artplayerPluginVttThumbnail },
      ] = await Promise.all([
        import("artplayer"),
        import("hls.js"),
        import("artplayer-plugin-danmuku"),
        import("artplayer-plugin-vtt-thumbnail"),
      ]);
      if (disposed || !containerRef.current) {
        return;
      }
      const themeColor = resolvePlayerThemeColor();

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

      const qualitySettings =
        sourceType === "m3u8" && qualities.length > 0
          ? [
              {
                name: "video-quality",
                html: "清晰度",
                tooltip: qualityLabel((qualities.find((item) => item.default) || qualities[0]).html),
                selector: qualities.map((item) => ({
                  html: item.html,
                  url: item.url,
                  default: Boolean(item.default),
                })),
                onSelect: async function (
                  this: { switchQuality?: (url: string) => Promise<void> },
                  item: { html: string | HTMLElement; url?: string },
                ) {
                  if (typeof item.url === "string" && item.url && typeof this.switchQuality === "function") {
                    await this.switchQuality(item.url);
                  }
                  return qualityLabel(item.html);
                },
              },
            ]
          : [];

      const plugins: Array<(art: any) => any> = [
        artplayerPluginDanmuku({
          danmuku: danmakuItemsRef.current.map(toPluginDanmu),
          emitter: canEmitDanmakuRef.current,
          maxLength: 200,
          lockTime: 5,
          beforeEmit: async (danmu) => {
            const content = (danmu.text || "").trim();
            if (!content) {
              return false;
            }
            if (!onEmitDanmakuRef.current) {
              return true;
            }
            try {
              const sent = await onEmitDanmakuRef.current({
                content,
                time_sec: Math.max(0, Number(danmu.time) || 0),
                mode: normalizeMode(danmu.mode),
                color: normalizeColor(danmu.color),
              });
              if (sent) {
                const input = containerRef.current?.querySelector<HTMLInputElement>(".apd-input");
                if (input) {
                  input.value = "";
                  input.dispatchEvent(new Event("input", { bubbles: true }));
                }
              }
            } catch {
              // Keep emitter stable; failed sends are handled by caller.
            }
            // Sending result comes from WS replay to avoid duplicate render.
            return false;
          },
        }),
      ];
      if (vttThumbnailUrl) {
        plugins.push(
          artplayerPluginVttThumbnail({
            vtt: vttThumbnailUrl,
          }),
        );
      }

      artInstance = new Artplayer({
        container: containerRef.current,
        url: sourceUrl,
        type: sourceType,
        customType,
        poster,
        theme: themeColor,
        cssVar: {
          "--art-theme": themeColor,
        },
        volume: 0.7,
        autoplay: false,
        autoSize: true,
        fullscreen: true,
        fullscreenWeb: true,
        playbackRate: true,
        screenshot: true,
        setting: true,
        settings: qualitySettings,
        plugins,
        moreVideoAttr: {
          crossOrigin: "anonymous",
        },
        pip: true,
        mutex: true,
      }) as ArtplayerInstance;

      danmakuPluginRef.current = artInstance.plugins?.artplayerPluginDanmuku ?? null;
      loadedDanmakuSignatureRef.current = danmakuItemsRef.current.map((item) => item.id).join("|");

      const video = artInstance.video as HTMLVideoElement;
      videoRef.current = video;
      const pendingLatest = pendingLatestDanmakuRef.current;
      if (danmakuPluginRef.current && pendingLatest?.id && latestDanmakuIDRef.current !== pendingLatest.id) {
        latestDanmakuIDRef.current = pendingLatest.id;
        danmakuPluginRef.current.emit(toRealtimeDanmu(pendingLatest, video.currentTime || 0));
        pendingLatestDanmakuRef.current = null;
      }

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
      danmakuPluginRef.current = null;
      if (hlsInstance) {
        hlsInstance.destroy();
      }
      if (artInstance) {
        artInstance.destroy(false);
      }
    };
  }, [poster, resolvedQualitySignature, sourceType, sourceUrl, vttThumbnailUrl]);

  useEffect(() => {
    const plugin = danmakuPluginRef.current;
    if (!plugin) {
      return;
    }
    if (loadedDanmakuSignatureRef.current === danmakuSignature) {
      return;
    }
    loadedDanmakuSignatureRef.current = danmakuSignature;
    void plugin.load(danmakuItems.map(toPluginDanmu));
  }, [danmakuItems, danmakuSignature]);

  useEffect(() => {
    const plugin = danmakuPluginRef.current;
    if (!plugin) {
      return;
    }
    plugin.config({
      ...plugin.option,
      emitter: canEmitDanmaku,
    });
  }, [canEmitDanmaku]);

  useEffect(() => {
    const plugin = danmakuPluginRef.current;
    if (!latestDanmaku || !latestDanmaku.id) {
      return;
    }
    if (latestDanmakuIDRef.current === latestDanmaku.id) {
      return;
    }
    if (!plugin) {
      pendingLatestDanmakuRef.current = latestDanmaku;
      return;
    }
    latestDanmakuIDRef.current = latestDanmaku.id;
    pendingLatestDanmakuRef.current = null;
    plugin.emit(toRealtimeDanmu(latestDanmaku, videoRef.current?.currentTime || 0));
  }, [latestDanmaku]);

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
