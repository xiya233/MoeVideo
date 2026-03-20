"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon, type IconName } from "@/components/common/AppIcon";
import { AuthorInline } from "@/components/common/AuthorInline";
import { EmptyState } from "@/components/common/EmptyState";
import { LiveBadge } from "@/components/common/LiveBadge";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import type { HomeData, VideoCard } from "@/lib/dto";
import { mapHomeData, mapVideoCard } from "@/lib/dto/mappers";
import { cn } from "@/lib/utils/cn";
import { formatCount, formatDate } from "@/lib/utils/format";

const FEATURED_TRANSITION_MS = 700;
const FEATURED_REDUCED_MOTION_MS = 180;

function formatDurationLabel(duration: number): string {
  const total = Math.max(0, Math.round(duration));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) {
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  }
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

function VideoMetricBadge({ icon, value }: { icon: IconName; value: string }) {
  return (
    <div className="inline-flex h-6 items-center rounded-[1rem] bg-black/50 px-2 py-1 backdrop-blur-md">
      <span className="inline-flex h-[14px] w-[14px] shrink-0 items-center justify-center">
        <AppIcon name={icon} size={14} className="shrink-0 text-white" />
      </span>
      <span className="ml-1.5 text-[10px] font-bold leading-[10px] tabular-nums text-white">{value}</span>
    </div>
  );
}

function VideoDurationBadge({ value }: { value: string }) {
  return (
    <div className="flex h-6 items-center rounded-[1rem] bg-black/50 px-2 py-1 backdrop-blur-md">
      <span className="text-[10px] font-bold tracking-wider text-white">{value}</span>
    </div>
  );
}

function VideoGridCard({ video }: { video: VideoCard }) {
  return (
    <article className="flex flex-col gap-3 group">
      <Link href={`/videos/${video.id}`} className="aspect-video rounded-xl overflow-hidden relative shadow-md">
        {video.cover_url || video.preview_webp_url ? (
          <>
            {video.cover_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={video.cover_url}
                alt={video.title}
                className={cn(
                  "h-full w-full object-cover transition-transform duration-500 group-hover:scale-105",
                  video.preview_webp_url ? "transition-opacity duration-200 group-hover:opacity-0" : "",
                )}
              />
            ) : (
              <div className="h-full w-full bg-primary/10" />
            )}

            {video.preview_webp_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={video.preview_webp_url}
                alt={`${video.title} 预览`}
                className="pointer-events-none absolute inset-0 h-full w-full object-cover opacity-0 transition-opacity duration-200 group-hover:opacity-100"
              />
            ) : null}
          </>
        ) : (
          <div className="h-full w-full bg-primary/10" />
        )}

        <div className="absolute bottom-2 left-2 flex items-center gap-2">
          <VideoMetricBadge icon="play_arrow" value={formatCount(video.views_count)} />
          <VideoMetricBadge icon="chat_bubble" value={formatCount(video.comments_count)} />
        </div>

        {video.is_live ? <LiveBadge className="absolute left-2 top-2" /> : null}

        <div className="absolute bottom-2 right-2">
          <VideoDurationBadge value={formatDurationLabel(video.duration_sec)} />
        </div>
      </Link>

      <div>
        <h3 className="mb-2 line-clamp-2 text-sm font-bold leading-tight transition-colors group-hover:text-primary">
          {video.title}
        </h3>
        <div className="flex items-center gap-2 opacity-70">
          <AuthorInline
            username={video.author.username}
            avatarUrl={video.author.avatar_url}
            href={video.author.id ? `/users/${video.author.id}` : undefined}
          />
          <span className="ml-auto text-[11px]">{formatDate(video.published_at)}</span>
        </div>
      </div>
    </article>
  );
}

function FeaturedHeroSlide({
  video,
  className,
}: {
  video: VideoCard | null;
  className?: string;
}) {
  return (
    <div className={cn("absolute inset-0", className)}>
      {video?.cover_url ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={video.cover_url} alt={video.title} className="h-full w-full object-cover" />
      ) : (
        <div className="h-full w-full bg-primary/10" />
      )}

      <div className="absolute inset-0 flex flex-col justify-end bg-gradient-to-t from-black/60 via-transparent to-transparent p-8">
        {video?.is_live ? <LiveBadge className="mb-3" /> : null}
        <span className="mb-3 w-fit rounded-full bg-primary px-3 py-1 text-xs font-bold text-white">
          今日推荐
        </span>
        {video ? (
          <>
            <Link
              href={`/videos/${video.id}`}
              className="mb-2 line-clamp-2 text-2xl font-bold text-white [text-shadow:0_2px_8px_rgba(0,0,0,0.45)] md:text-3xl"
              style={{ color: "#ffffff" }}
            >
              <span className="text-white">{video.title}</span>
            </Link>
            <p className="max-w-lg text-sm text-white/90">
              来自 {video.author.username} · {formatCount(video.views_count)} 播放
            </p>
          </>
        ) : (
          <p className="text-sm text-white/80">暂无推荐视频</p>
        )}
      </div>
    </div>
  );
}

type HomePageProps = {
  query?: string;
  category?: string;
};

export function HomePage({ query = "", category = "" }: HomePageProps) {
  const { request } = useAuth();
  const router = useRouter();
  const loadMoreTriggerRef = useRef<HTMLDivElement | null>(null);
  const loadingMoreRef = useRef(false);
  const filterKeyRef = useRef("");
  const featuredTransitionTimerRef = useRef<number | null>(null);

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [home, setHome] = useState<HomeData | null>(null);
  const [videos, setVideos] = useState<VideoCard[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState("");
  const [initialLoaded, setInitialLoaded] = useState(false);
  const [currentFeaturedIndex, setCurrentFeaturedIndex] = useState(0);
  const [nextFeaturedIndex, setNextFeaturedIndex] = useState<number | null>(null);
  const [isFeaturedTransitioning, setIsFeaturedTransitioning] = useState(false);
  const [transitionDirection, setTransitionDirection] = useState<"next" | "prev" | "dot">("next");
  const [carouselPaused, setCarouselPaused] = useState(false);
  const [prefersReducedMotion, setPrefersReducedMotion] = useState(false);

  const q = query.trim();
  const categoryFilter = category.trim();
  const filterKey = `${q}::${categoryFilter}`;

  useEffect(() => {
    filterKeyRef.current = filterKey;
  }, [filterKey]);

  const fetchHome = useCallback(async () => {
    const requestFilterKey = filterKey;
    filterKeyRef.current = requestFilterKey;
    loadingMoreRef.current = false;
    setLoading(true);
    setError("");
    setLoadMoreError("");
    setLoadingMore(false);
    setInitialLoaded(false);
    setVideos([]);
    setNextCursor("");
    if (typeof window !== "undefined") {
      window.scrollTo({ top: 0, behavior: "auto" });
    }

    const params = new URLSearchParams();
    if (q) {
      params.set("q", q);
    }
    if (categoryFilter) {
      params.set("category", categoryFilter);
    }

    try {
      const data = await request<HomeData>(`/home${params.toString() ? `?${params.toString()}` : ""}`, {
        auth: false,
      });
      if (filterKeyRef.current !== requestFilterKey) {
        return;
      }
      const mappedHome = mapHomeData(data);
      setHome(mappedHome);
      setVideos(mappedHome.videos);
      setNextCursor(mappedHome.next_cursor ?? "");
      setInitialLoaded(true);
    } catch (err) {
      if (filterKeyRef.current !== requestFilterKey) {
        return;
      }
      const message = err instanceof Error ? err.message : "加载首页失败";
      setError(message);
      setHome(null);
      setVideos([]);
      setNextCursor("");
    } finally {
      if (filterKeyRef.current === requestFilterKey) {
        setLoading(false);
      }
    }
  }, [categoryFilter, filterKey, q, request]);

  useEffect(() => {
    void fetchHome();
  }, [fetchHome]);

  const fetchMoreVideos = useCallback(async () => {
    if (loading || error || loadingMoreRef.current || !nextCursor) {
      return;
    }
    const requestFilterKey = filterKey;
    loadingMoreRef.current = true;
    setLoadingMore(true);
    setLoadMoreError("");

    const params = new URLSearchParams();
    params.set("cursor", nextCursor);
    params.set("limit", "20");
    if (q) {
      params.set("q", q);
    }
    if (categoryFilter) {
      params.set("category", categoryFilter);
    }

    try {
      const data = await request<{ items: unknown[]; next_cursor?: string }>(`/videos?${params.toString()}`, {
        auth: false,
      });
      if (filterKeyRef.current !== requestFilterKey) {
        return;
      }

      const incoming = (data.items ?? []).map(mapVideoCard);
      setVideos((prev) => {
        if (incoming.length === 0) {
          return prev;
        }
        const seen = new Set(prev.map((item) => item.id));
        const merged = [...prev];
        for (const item of incoming) {
          if (seen.has(item.id)) {
            continue;
          }
          seen.add(item.id);
          merged.push(item);
        }
        return merged;
      });
      setNextCursor(data.next_cursor ?? "");
    } catch (err) {
      if (filterKeyRef.current !== requestFilterKey) {
        return;
      }
      setLoadMoreError(err instanceof Error ? err.message : "加载更多失败");
    } finally {
      if (filterKeyRef.current === requestFilterKey) {
        loadingMoreRef.current = false;
        setLoadingMore(false);
      }
    }
  }, [categoryFilter, error, filterKey, loading, nextCursor, q, request]);

  useEffect(() => {
    const trigger = loadMoreTriggerRef.current;
    if (!trigger || !initialLoaded || loading || error || !nextCursor) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
          return;
        }
        void fetchMoreVideos();
      },
      {
        root: null,
        rootMargin: "800px 0px",
        threshold: 0,
      },
    );
    observer.observe(trigger);
    return () => observer.disconnect();
  }, [error, fetchMoreVideos, initialLoaded, loading, nextCursor]);

  const setCategoryFilter = (value: string) => {
    const params = new URLSearchParams();
    if (q) {
      params.set("q", q);
    }
    if (!value) {
      params.delete("category");
    } else {
      params.set("category", value);
    }
    const queryString = params.toString();
    router.push(queryString ? `/?${queryString}` : "/");
  };

  const ranking = useMemo(() => home?.hot_rankings?.slice(0, 5) ?? [], [home?.hot_rankings]);
  const featuredItems = useMemo(() => {
    if (home?.featured_items && home.featured_items.length > 0) {
      return home.featured_items.slice(0, 5);
    }
    return home?.featured ? [home.featured] : [];
  }, [home?.featured, home?.featured_items]);
  const featuredSignature = useMemo(() => featuredItems.map((item) => item.id).join(","), [featuredItems]);

  const clearFeaturedTransitionTimer = useCallback(() => {
    if (featuredTransitionTimerRef.current === null) {
      return;
    }
    window.clearTimeout(featuredTransitionTimerRef.current);
    featuredTransitionTimerRef.current = null;
  }, []);

  const safeCurrentFeaturedIndex = useMemo(() => {
    if (featuredItems.length <= 0) {
      return 0;
    }
    return Math.min(currentFeaturedIndex, featuredItems.length - 1);
  }, [currentFeaturedIndex, featuredItems.length]);

  const activeFeatured = featuredItems.length > 0 ? featuredItems[safeCurrentFeaturedIndex] : null;
  const transitioningFeatured =
    nextFeaturedIndex !== null && nextFeaturedIndex >= 0 && nextFeaturedIndex < featuredItems.length
      ? featuredItems[nextFeaturedIndex]
      : null;
  const displayedFeaturedIndex =
    isFeaturedTransitioning && nextFeaturedIndex !== null ? nextFeaturedIndex : safeCurrentFeaturedIndex;
  const featuredEnterClass =
    transitionDirection === "prev"
      ? "hero-featured-enter-prev"
      : transitionDirection === "dot"
        ? "hero-featured-enter-dot"
        : "hero-featured-enter-next";
  const featuredExitClass =
    transitionDirection === "prev"
      ? "hero-featured-exit-prev"
      : transitionDirection === "dot"
        ? "hero-featured-exit-dot"
        : "hero-featured-exit-next";

  const startFeaturedTransition = useCallback(
    (targetIndex: number, direction: "next" | "prev" | "dot") => {
      if (featuredItems.length <= 1 || isFeaturedTransitioning) {
        return;
      }
      const bounded = Math.max(0, Math.min(targetIndex, featuredItems.length - 1));
      if (bounded === safeCurrentFeaturedIndex) {
        return;
      }

      clearFeaturedTransitionTimer();
      setTransitionDirection(direction);
      setNextFeaturedIndex(bounded);
      setIsFeaturedTransitioning(true);

      const duration = prefersReducedMotion ? FEATURED_REDUCED_MOTION_MS : FEATURED_TRANSITION_MS;
      featuredTransitionTimerRef.current = window.setTimeout(() => {
        setCurrentFeaturedIndex(bounded);
        setNextFeaturedIndex(null);
        setIsFeaturedTransitioning(false);
        featuredTransitionTimerRef.current = null;
      }, duration);
    },
    [
      clearFeaturedTransitionTimer,
      featuredItems.length,
      isFeaturedTransitioning,
      prefersReducedMotion,
      safeCurrentFeaturedIndex,
    ],
  );

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const onChange = () => setPrefersReducedMotion(media.matches);
    onChange();
    media.addEventListener("change", onChange);
    return () => {
      media.removeEventListener("change", onChange);
    };
  }, []);

  useEffect(() => {
    clearFeaturedTransitionTimer();
    setCurrentFeaturedIndex(0);
    setNextFeaturedIndex(null);
    setIsFeaturedTransitioning(false);
    setTransitionDirection("next");
  }, [clearFeaturedTransitionTimer, featuredSignature]);

  useEffect(() => {
    if (currentFeaturedIndex === safeCurrentFeaturedIndex) {
      return;
    }
    setCurrentFeaturedIndex(safeCurrentFeaturedIndex);
  }, [currentFeaturedIndex, safeCurrentFeaturedIndex]);

  useEffect(() => {
    return () => clearFeaturedTransitionTimer();
  }, [clearFeaturedTransitionTimer]);

  useEffect(() => {
    if (carouselPaused || featuredItems.length <= 1) {
      return;
    }
    const timer = window.setInterval(() => {
      if (isFeaturedTransitioning) {
        return;
      }
      const targetIndex = (safeCurrentFeaturedIndex + 1) % featuredItems.length;
      startFeaturedTransition(targetIndex, "next");
    }, 4000);
    return () => {
      window.clearInterval(timer);
    };
  }, [
    carouselPaused,
    featuredItems.length,
    isFeaturedTransitioning,
    safeCurrentFeaturedIndex,
    startFeaturedTransition,
  ]);

  const goToNextFeatured = () => {
    if (featuredItems.length <= 1 || isFeaturedTransitioning) {
      return;
    }
    const targetIndex = (safeCurrentFeaturedIndex + 1) % featuredItems.length;
    startFeaturedTransition(targetIndex, "next");
  };

  const goToPrevFeatured = () => {
    if (featuredItems.length <= 1 || isFeaturedTransitioning) {
      return;
    }
    const targetIndex = (safeCurrentFeaturedIndex - 1 + featuredItems.length) % featuredItems.length;
    startFeaturedTransition(targetIndex, "prev");
  };

  return (
    <div className="space-y-12">
      {loading ? (
        <section className="grid grid-cols-1 gap-6 lg:grid-cols-12">
          <LoadingSkeleton className="h-[360px] lg:col-span-8" />
          <LoadingSkeleton className="h-[360px] lg:col-span-4" />
        </section>
      ) : null}

      {!loading && error ? <EmptyState title="首页加载失败" description={error} /> : null}

      {!loading && !error ? (
        <>
          <section className="grid grid-cols-1 gap-6 lg:grid-cols-12 lg:items-stretch">
            <div className="group relative lg:col-span-8">
              <div
                className="relative h-[320px] w-full overflow-hidden rounded-xl bg-primary/5 shadow-xl md:h-[360px] lg:h-[420px]"
                onMouseEnter={() => setCarouselPaused(true)}
                onMouseLeave={() => setCarouselPaused(false)}
              >
                <FeaturedHeroSlide
                  video={activeFeatured}
                  className={cn("hero-featured-layer z-10", isFeaturedTransitioning ? featuredExitClass : "opacity-100")}
                />

                {isFeaturedTransitioning && transitioningFeatured ? (
                  <FeaturedHeroSlide video={transitioningFeatured} className={cn("hero-featured-layer z-20", featuredEnterClass)} />
                ) : null}

                {isFeaturedTransitioning ? <span aria-hidden className="hero-featured-sweep z-30" /> : null}

                {featuredItems.length > 1 ? (
                  <>
                    <button
                      type="button"
                      aria-label="上一张"
                      onClick={goToPrevFeatured}
                      disabled={isFeaturedTransitioning}
                      className="absolute left-4 top-1/2 z-40 inline-flex h-9 w-9 -translate-y-1/2 items-center justify-center rounded-full bg-black/35 text-white backdrop-blur-md transition hover:bg-black/55"
                    >
                      <AppIcon name="chevron_left" size={18} />
                    </button>
                    <button
                      type="button"
                      aria-label="下一张"
                      onClick={goToNextFeatured}
                      disabled={isFeaturedTransitioning}
                      className="absolute right-4 top-1/2 z-40 inline-flex h-9 w-9 -translate-y-1/2 items-center justify-center rounded-full bg-black/35 text-white backdrop-blur-md transition hover:bg-black/55"
                    >
                      <AppIcon name="chevron_right" size={18} />
                    </button>
                  </>
                ) : null}

                <div className="absolute bottom-4 right-8 z-40 flex gap-2">
                  {(featuredItems.length > 0 ? featuredItems : Array.from({ length: 1 })).map((_, idx) => (
                    <button
                      key={idx}
                      type="button"
                      aria-label={`切换到第 ${idx + 1} 张`}
                      onClick={() =>
                        startFeaturedTransition(
                          idx,
                          idx > safeCurrentFeaturedIndex ? "next" : idx < safeCurrentFeaturedIndex ? "prev" : "dot",
                        )
                      }
                      disabled={isFeaturedTransitioning}
                      className={cn(
                        "h-2 w-2 rounded-full transition",
                        idx === displayedFeaturedIndex ? "bg-white" : "bg-white/50 hover:bg-white/70",
                      )}
                    />
                  ))}
                </div>
              </div>
            </div>

            <aside className="flex h-[320px] flex-col rounded-xl border border-slate-100 bg-white p-6 shadow-sm md:h-[360px] lg:col-span-4 lg:h-[420px]">
              <div className="mb-6 flex items-center justify-between">
                <h2 className="flex items-center gap-2 text-lg font-bold text-slate-900">
                  <AppIcon name="local_fire_department" className="text-orange-500" />
                  热度排行榜
                </h2>
              </div>

              <div className="space-y-3">
                {ranking.map((video, index) => (
                  <Link key={video.id} href={`/videos/${video.id}`} className="group flex cursor-pointer items-center gap-4">
                    <span
                      className={cn(
                        "w-6 text-xl font-black italic",
                        index === 0
                          ? "text-orange-500"
                          : index === 1
                            ? "text-orange-400"
                            : index === 2
                              ? "text-orange-300"
                              : "text-slate-300",
                      )}
                    >
                      {index + 1}
                    </span>
                    <div className="relative h-10 w-16 flex-shrink-0 overflow-hidden rounded-lg">
                      {video.cover_url ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img
                          src={video.cover_url}
                          alt={video.title}
                          className="h-full w-full object-cover transition-transform group-hover:scale-110"
                        />
                      ) : (
                        <div className="h-full w-full bg-primary/10" />
                      )}
                      {video.is_live ? <LiveBadge className="absolute left-1 top-1 scale-75 origin-left" /> : null}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-bold transition-colors group-hover:text-primary">{video.title}</p>
                      <p className="text-[10px] text-slate-400">{formatCount(video.views_count)} 播放</p>
                    </div>
                  </Link>
                ))}

                {ranking.length === 0 ? <p className="text-sm text-slate-500">暂无排行数据</p> : null}
              </div>

              <div className="mt-auto flex justify-center pt-4">
                <Link
                  className="inline-flex items-center gap-1.5 rounded-full bg-primary px-5 py-2 text-sm font-bold text-white shadow-lg shadow-primary/25 transition-all hover:scale-105 hover:text-white active:scale-95 active:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/70 focus-visible:ring-offset-2"
                  href="/rankings/hot"
                  style={{ color: "#ffffff" }}
                >
                  <span className="text-white">查看更多</span>
                  <AppIcon name="chevron_right" size={16} className="text-white" />
                </Link>
              </div>
            </aside>
          </section>

          <section id="categories" className="flex items-center gap-3 overflow-x-auto pb-2 no-scrollbar">
            <button
              type="button"
              onClick={() => setCategoryFilter("")}
              className={cn(
                "whitespace-nowrap rounded-full px-6 py-2 text-sm font-bold transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2",
                !categoryFilter
                  ? "bg-primary text-white"
                  : "border border-slate-100 bg-white font-medium hover:border-primary",
              )}
            >
              全部
            </button>

            {(home?.categories ?? []).map((item) => {
              const active = categoryFilter === item.slug || categoryFilter === String(item.id);
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setCategoryFilter(item.slug)}
                  className={cn(
                    "whitespace-nowrap rounded-full px-6 py-2 text-sm transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2",
                    active
                      ? "bg-primary font-bold text-white"
                      : "border border-slate-100 bg-white font-medium hover:border-primary",
                  )}
                >
                  {item.name}
                </button>
              );
            })}
          </section>

          {videos.length === 0 ? (
            <EmptyState title="暂无视频" description="试试更换分类或搜索关键词。" />
          ) : (
            <>
              <section className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
                {videos.map((video) => (
                  <VideoGridCard key={video.id} video={video} />
                ))}
              </section>

              {loadingMore ? (
                <section className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
                  {Array.from({ length: 4 }).map((_, idx) => (
                    <LoadingSkeleton key={idx} className="aspect-video rounded-xl" />
                  ))}
                </section>
              ) : null}

              {loadMoreError ? (
                <div className="flex flex-col items-center gap-3 rounded-xl border border-red-100 bg-red-50/60 p-4 text-sm text-red-600">
                  <p>{loadMoreError}</p>
                  <button
                    type="button"
                    onClick={() => void fetchMoreVideos()}
                    className="rounded-lg border border-red-200 bg-white px-4 py-1.5 font-semibold text-red-600 transition hover:bg-red-50"
                  >
                    重试加载
                  </button>
                </div>
              ) : null}

              {!nextCursor && videos.length > 0 && initialLoaded ? (
                <p className="py-2 text-center text-sm text-slate-400">没有更多视频了</p>
              ) : null}

              <div ref={loadMoreTriggerRef} aria-hidden className="h-px w-full" />
            </>
          )}
        </>
      ) : null}
    </div>
  );
}
