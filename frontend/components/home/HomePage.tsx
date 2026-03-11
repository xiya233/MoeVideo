"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon, type IconName } from "@/components/common/AppIcon";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import type { HomeData, VideoCard } from "@/lib/dto";
import { mapHomeData } from "@/lib/dto/mappers";
import { cn } from "@/lib/utils/cn";
import { formatCount, formatDate } from "@/lib/utils/format";

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

        <div className="absolute bottom-2 right-2">
          <VideoDurationBadge value={formatDurationLabel(video.duration_sec)} />
        </div>
      </Link>

      <div>
        <h3 className="mb-2 line-clamp-2 text-sm font-bold leading-tight transition-colors group-hover:text-primary">
          {video.title}
        </h3>
        <div className="flex items-center gap-2 opacity-70">
          <div className="flex h-5 w-5 items-center justify-center rounded-full bg-primary/20">
            <AppIcon name="person" size={12} className="text-primary" />
          </div>
          <span className="text-[11px] font-medium">{video.author.username}</span>
          <span className="ml-auto text-[11px]">{formatDate(video.published_at)}</span>
        </div>
      </div>
    </article>
  );
}

type HomePageProps = {
  query?: string;
  category?: string;
};

export function HomePage({ query = "", category = "" }: HomePageProps) {
  const { request } = useAuth();
  const router = useRouter();

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [home, setHome] = useState<HomeData | null>(null);

  const q = query.trim();
  const categoryFilter = category.trim();

  const fetchHome = useCallback(async () => {
    setLoading(true);
    setError("");

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
      setHome(mapHomeData(data));
    } catch (err) {
      const message = err instanceof Error ? err.message : "加载首页失败";
      setError(message);
      setHome(null);
    } finally {
      setLoading(false);
    }
  }, [categoryFilter, q, request]);

  useEffect(() => {
    void fetchHome();
  }, [fetchHome]);

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
          <section className="grid grid-cols-1 gap-6 lg:grid-cols-12">
            <div className="group relative lg:col-span-8">
              <div className="relative aspect-[16/9] w-full overflow-hidden rounded-xl bg-primary/5 shadow-xl">
                {home?.featured?.cover_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img
                    src={home.featured.cover_url}
                    alt={home.featured.title}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <div className="h-full w-full bg-primary/10" />
                )}

                <div className="absolute inset-0 flex flex-col justify-end bg-gradient-to-t from-black/60 via-transparent to-transparent p-8">
                  <span className="mb-3 w-fit rounded-full bg-primary px-3 py-1 text-xs font-bold text-white">
                    今日推荐
                  </span>
                  {home?.featured ? (
                    <>
                      <Link href={`/videos/${home.featured.id}`} className="mb-2 line-clamp-2 text-3xl font-bold text-white">
                        {home.featured.title}
                      </Link>
                      <p className="max-w-lg text-sm text-white/80">
                        来自 {home.featured.author.username} · {formatCount(home.featured.views_count)} 播放
                      </p>
                    </>
                  ) : (
                    <p className="text-sm text-white/80">暂无推荐视频</p>
                  )}
                </div>

                <div className="absolute bottom-4 right-8 flex gap-2">
                  <div className="h-2 w-2 rounded-full bg-white" />
                  <div className="h-2 w-2 rounded-full bg-white/50" />
                  <div className="h-2 w-2 rounded-full bg-white/50" />
                </div>
              </div>
            </div>

            <aside className="rounded-xl border border-slate-100 bg-white p-6 shadow-sm lg:col-span-4">
              <div className="mb-6 flex items-center justify-between">
                <h2 className="flex items-center gap-2 text-lg font-bold text-slate-900">
                  <AppIcon name="local_fire_department" className="text-orange-500" />
                  热度排行榜
                </h2>
                <Link className="text-xs font-medium text-primary" href="/">
                  查看更多
                </Link>
              </div>

              <div className="space-y-4">
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
                    <div className="h-10 w-16 flex-shrink-0 overflow-hidden rounded-lg">
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
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-bold transition-colors group-hover:text-primary">{video.title}</p>
                      <p className="text-[10px] text-slate-400">{formatCount(video.views_count)} 播放</p>
                    </div>
                  </Link>
                ))}

                {ranking.length === 0 ? <p className="text-sm text-slate-500">暂无排行数据</p> : null}
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

          {(home?.videos?.length ?? 0) === 0 ? (
            <EmptyState title="暂无视频" description="试试更换分类或搜索关键词。" />
          ) : (
            <section className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
              {(home?.videos ?? []).map((video) => (
                <VideoGridCard key={video.id} video={video} />
              ))}
            </section>
          )}
        </>
      ) : null}
    </div>
  );
}
