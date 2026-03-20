"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AuthorInline } from "@/components/common/AuthorInline";
import { EmptyState } from "@/components/common/EmptyState";
import { LiveBadge } from "@/components/common/LiveBadge";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import { AppIcon } from "@/components/common/AppIcon";
import type { HotRankingsData, VideoCard } from "@/lib/dto";
import { mapHotRankingsData } from "@/lib/dto/mappers";
import { cn } from "@/lib/utils/cn";
import { formatCount } from "@/lib/utils/format";

function HotRankingCard({ video, rank }: { video: VideoCard; rank: number }) {
  return (
    <Link
      href={`/videos/${video.id}`}
      className="group grid grid-cols-[44px_160px_minmax(0,1fr)] items-center gap-4 rounded-2xl border border-slate-100 bg-white p-4 transition hover:border-primary/30 hover:shadow-sm"
    >
      <span
        className={cn(
          "text-center text-2xl font-black italic",
          rank === 1
            ? "text-orange-500"
            : rank === 2
              ? "text-orange-400"
              : rank === 3
                ? "text-orange-300"
                : "text-slate-300",
        )}
      >
        {rank}
      </span>

      <div className="relative aspect-video overflow-hidden rounded-xl bg-primary/10">
        {video.cover_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={video.cover_url} alt={video.title} className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105" />
        ) : (
          <div className="h-full w-full bg-primary/10" />
        )}
        {video.is_live ? <LiveBadge className="absolute left-2 top-2" /> : null}
      </div>

      <div className="min-w-0 space-y-1.5">
        <h3 className="line-clamp-2 text-base font-bold text-slate-900 transition-colors group-hover:text-primary">{video.title}</h3>
        <div className="flex items-center gap-2 text-sm text-slate-500">
          <AuthorInline
            username={video.author.username}
            avatarUrl={video.author.avatar_url}
            avatarClassName="h-6 w-6"
            usernameClassName="text-xs text-slate-500"
          />
        </div>
        <p className="text-xs text-slate-400">{formatCount(video.views_count)} 播放 · {formatCount(video.comments_count)} 评论</p>
      </div>
    </Link>
  );
}

export function HotRankingsPage() {
  const { request } = useAuth();
  const loadMoreTriggerRef = useRef<HTMLDivElement | null>(null);
  const loadingMoreRef = useRef(false);

  const [items, setItems] = useState<VideoCard[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");

  const fetchFirstPage = useCallback(async () => {
    setLoading(true);
    setError("");
    setItems([]);
    setNextCursor("");
    loadingMoreRef.current = false;
    setLoadingMore(false);
    try {
      const raw = await request<HotRankingsData>("/rankings/hot?limit=20", { auth: false });
      const data = mapHotRankingsData(raw);
      setItems(data.items);
      setNextCursor(data.next_cursor ?? "");
    } catch (err) {
      setError(err instanceof Error ? err.message : "热榜加载失败");
    } finally {
      setLoading(false);
    }
  }, [request]);

  useEffect(() => {
    void fetchFirstPage();
  }, [fetchFirstPage]);

  const fetchMore = useCallback(async () => {
    if (!nextCursor || loading || loadingMoreRef.current || error) {
      return;
    }
    loadingMoreRef.current = true;
    setLoadingMore(true);
    try {
      const raw = await request<HotRankingsData>(`/rankings/hot?limit=20&cursor=${encodeURIComponent(nextCursor)}`, { auth: false });
      const data = mapHotRankingsData(raw);
      setItems((prev) => {
        const seen = new Set(prev.map((item) => item.id));
        const merged = [...prev];
        for (const item of data.items) {
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
      setError(err instanceof Error ? err.message : "热榜加载失败");
    } finally {
      loadingMoreRef.current = false;
      setLoadingMore(false);
    }
  }, [error, loading, nextCursor, request]);

  useEffect(() => {
    const trigger = loadMoreTriggerRef.current;
    if (!trigger || !nextCursor || loading || error) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
          return;
        }
        void fetchMore();
      },
      {
        root: null,
        rootMargin: "800px 0px",
        threshold: 0,
      },
    );
    observer.observe(trigger);
    return () => observer.disconnect();
  }, [error, fetchMore, loading, nextCursor]);

  const hasMore = useMemo(() => nextCursor.trim() !== "", [nextCursor]);

  return (
    <div className="space-y-6">
      <header className="rounded-2xl border border-primary/10 bg-white p-6 shadow-sm">
        <Link href="/" className="mb-4 inline-flex items-center gap-1 text-sm font-medium text-primary hover:text-primary/80">
          <AppIcon name="chevron_left" size={16} />
          返回首页
        </Link>
        <h1 className="flex items-center gap-2 text-2xl font-bold text-slate-900">
          <AppIcon name="local_fire_department" className="text-orange-500" size={22} />
          热度排行榜
        </h1>
        <p className="mt-2 text-sm text-slate-500">按热度分值排序，持续滚动加载更多热门视频。</p>
      </header>

      {loading ? (
        <section className="space-y-4">
          {Array.from({ length: 6 }).map((_, idx) => (
            <LoadingSkeleton key={idx} className="h-[124px] rounded-2xl" />
          ))}
        </section>
      ) : null}

      {!loading && error ? <EmptyState title="热榜加载失败" description={error} /> : null}

      {!loading && !error ? (
        <>
          {items.length === 0 ? (
            <EmptyState title="暂无热榜数据" description="当前还没有可展示的热门视频。" />
          ) : (
            <section className="space-y-4">
              {items.map((video, index) => (
                <HotRankingCard key={video.id} video={video} rank={index + 1} />
              ))}
            </section>
          )}

          {loadingMore ? (
            <section className="space-y-4">
              {Array.from({ length: 3 }).map((_, idx) => (
                <LoadingSkeleton key={idx} className="h-[124px] rounded-2xl" />
              ))}
            </section>
          ) : null}

          {!hasMore && items.length > 0 ? (
            <p className="py-1 text-center text-sm text-slate-400">没有更多排行视频了</p>
          ) : null}

          <div ref={loadMoreTriggerRef} aria-hidden className="h-px w-full" />
        </>
      ) : null}
    </div>
  );
}
