"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import type { HomeData, VideoCard } from "@/lib/dto";
import { mapHomeData } from "@/lib/dto/mappers";
import { formatCount, formatDate } from "@/lib/utils/format";

function VideoGridCard({ video }: { video: VideoCard }) {
  return (
    <article className="group flex flex-col gap-3">
      <Link href={`/videos/${video.id}`} className="relative aspect-video overflow-hidden rounded-xl shadow-md">
        {video.cover_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={video.cover_url}
            alt={video.title}
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
          />
        ) : (
          <div className="h-full w-full bg-gradient-to-br from-sky-100 via-cyan-50 to-indigo-100" />
        )}

        <div className="absolute bottom-2 left-2 flex items-center gap-2">
          <div className="flex h-6 items-center gap-1.5 rounded-lg bg-black/50 px-2 py-1 backdrop-blur-md">
            <span className="material-symbols-outlined text-[14px] text-white">play_arrow</span>
            <span className="text-[10px] font-bold text-white">{formatCount(video.views_count)}</span>
          </div>
          <div className="flex h-6 items-center gap-1.5 rounded-lg bg-black/50 px-2 py-1 backdrop-blur-md">
            <span className="material-symbols-outlined text-[14px] text-white">chat_bubble</span>
            <span className="text-[10px] font-bold text-white">{formatCount(video.comments_count)}</span>
          </div>
        </div>

        <div className="absolute bottom-2 right-2 rounded-lg bg-black/50 px-2 py-1 backdrop-blur-md">
          <span className="text-[10px] font-bold tracking-wider text-white">{Math.max(video.duration_sec, 0)}s</span>
        </div>
      </Link>

      <div>
        <h3 className="line-clamp-2 text-sm font-bold leading-tight text-slate-900 transition-colors group-hover:text-sky-500">
          {video.title}
        </h3>
        <div className="mt-2 flex items-center gap-2 opacity-80">
          <div className="flex h-5 w-5 items-center justify-center rounded-full bg-sky-100">
            <span className="material-symbols-outlined text-[12px] text-sky-700">person</span>
          </div>
          <span className="text-[11px] font-medium text-slate-600">{video.author.username}</span>
          <span className="ml-auto text-[11px] text-slate-500">{formatDate(video.published_at)}</span>
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
    const query = params.toString();
    router.push(query ? `/?${query}` : "/");
  };

  const ranking = useMemo(() => home?.hot_rankings?.slice(0, 5) ?? [], [home?.hot_rankings]);

  return (
    <div className="space-y-8">
      {loading ? (
        <section className="grid grid-cols-1 gap-6 lg:grid-cols-12">
          <LoadingSkeleton className="h-[380px] lg:col-span-8" />
          <LoadingSkeleton className="h-[380px] lg:col-span-4" />
        </section>
      ) : null}

      {!loading && error ? (
        <EmptyState title="首页加载失败" description={error} />
      ) : null}

      {!loading && !error ? (
        <>
          <section className="grid grid-cols-1 gap-6 lg:grid-cols-12">
            <div className="group relative lg:col-span-8">
              <div className="relative aspect-[16/9] w-full overflow-hidden rounded-xl bg-sky-100 shadow-xl">
                {home?.featured?.cover_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img
                    src={home.featured.cover_url}
                    alt={home.featured.title}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <div className="h-full w-full bg-gradient-to-br from-cyan-100 via-sky-50 to-indigo-100" />
                )}
                <div className="absolute inset-0 flex flex-col justify-end bg-gradient-to-t from-black/65 via-transparent to-transparent p-8">
                  <span className="mb-3 w-fit rounded-full bg-sky-500 px-3 py-1 text-xs font-bold text-white">
                    今日推荐
                  </span>
                  {home?.featured ? (
                    <>
                      <Link href={`/videos/${home.featured.id}`} className="line-clamp-2 text-3xl font-bold text-white">
                        {home.featured.title}
                      </Link>
                      <p className="mt-2 text-sm text-white/80">来自 {home.featured.author.username} · {formatCount(home.featured.views_count)} 播放</p>
                    </>
                  ) : (
                    <p className="text-sm text-white/85">暂无推荐视频</p>
                  )}
                </div>
              </div>
            </div>

            <aside className="rounded-xl border border-slate-100 bg-white p-6 shadow-sm lg:col-span-4">
              <div className="mb-6 flex items-center justify-between">
                <h2 className="flex items-center gap-2 text-lg font-bold text-slate-900">
                  <span className="material-symbols-outlined text-orange-500" style={{ fontVariationSettings: "'FILL' 1" }}>
                    local_fire_department
                  </span>
                  热度排行榜
                </h2>
                <Link className="text-xs font-medium text-sky-600" href="/">
                  查看更多
                </Link>
              </div>

              <div className="space-y-4">
                {ranking.map((video, index) => (
                  <Link key={video.id} href={`/videos/${video.id}`} className="group flex items-center gap-4">
                    <span
                      className={`w-6 text-xl font-black italic ${
                        index === 0
                          ? "text-orange-500"
                          : index === 1
                            ? "text-orange-400"
                            : index === 2
                              ? "text-orange-300"
                              : "text-slate-300"
                      }`}
                    >
                      {index + 1}
                    </span>
                    <div className="h-10 w-16 overflow-hidden rounded-lg">
                      {video.cover_url ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img
                          src={video.cover_url}
                          alt={video.title}
                          className="h-full w-full object-cover transition-transform group-hover:scale-110"
                        />
                      ) : (
                        <div className="h-full w-full bg-gradient-to-br from-slate-100 to-slate-200" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-bold text-slate-800 transition-colors group-hover:text-sky-500">
                        {video.title}
                      </p>
                      <p className="text-[10px] text-slate-400">{formatCount(video.views_count)} 播放</p>
                    </div>
                  </Link>
                ))}
                {ranking.length === 0 ? <p className="text-sm text-slate-500">暂无排行数据</p> : null}
              </div>
            </aside>
          </section>

          <section id="categories" className="flex items-center gap-3 overflow-x-auto pb-2">
            <button
              type="button"
              onClick={() => setCategoryFilter("")}
              className={`whitespace-nowrap rounded-full px-6 py-2 text-sm font-bold transition ${
                  !categoryFilter
                  ? "bg-sky-500 text-white"
                  : "border border-slate-200 bg-white text-slate-700 hover:border-sky-300"
              }`}
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
                  className={`whitespace-nowrap rounded-full px-6 py-2 text-sm font-medium transition ${
                    active
                      ? "bg-sky-500 text-white"
                      : "border border-slate-200 bg-white text-slate-700 hover:border-sky-300"
                  }`}
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
