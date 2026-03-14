"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AuthorInline } from "@/components/common/AuthorInline";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import type { TagItem, TagsData, VideoCard } from "@/lib/dto";
import { mapTagsData, mapVideoCard } from "@/lib/dto/mappers";
import { cn } from "@/lib/utils/cn";
import { formatCount, formatDate } from "@/lib/utils/format";

const TAGS_LIMIT = 50;
const VIDEOS_LIMIT = 20;

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

function TagsVideoCard({ video }: { video: VideoCard }) {
  return (
    <article className="flex flex-col gap-3 group">
      <Link href={`/videos/${video.id}`} className="relative aspect-video overflow-hidden rounded-xl bg-primary/5 shadow-sm">
        {video.cover_url || video.preview_webp_url ? (
          <>
            {video.cover_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={video.cover_url}
                alt={video.title}
                className={cn(
                  "h-full w-full object-cover transition-transform duration-300 group-hover:scale-105",
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
      </Link>

      <div className="space-y-2">
        <h3 className="line-clamp-2 text-sm font-bold leading-tight transition-colors group-hover:text-primary">{video.title}</h3>
        <div className="flex items-center gap-2">
          <AuthorInline
            username={video.author.username}
            avatarUrl={video.author.avatar_url}
            href={video.author.id ? `/users/${video.author.id}` : undefined}
            avatarClassName="h-5 w-5"
            usernameClassName="text-[11px] text-slate-500"
          />
          <span className="ml-auto text-[11px] text-slate-400">{formatDate(video.published_at)}</span>
        </div>
        <div className="flex items-center gap-3 text-[11px] text-slate-400">
          <span>{formatCount(video.views_count)} 播放</span>
          <span>{formatCount(video.comments_count)} 评论</span>
          <span>{formatDurationLabel(video.duration_sec)}</span>
        </div>
      </div>
    </article>
  );
}

export function TagsPage() {
  const { request } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const selectedTag = useMemo(() => (searchParams.get("tag") || "").trim(), [searchParams]);

  const [tags, setTags] = useState<TagItem[]>([]);
  const [tagsNextCursor, setTagsNextCursor] = useState("");
  const [tagsLoading, setTagsLoading] = useState(true);
  const [tagsLoadingMore, setTagsLoadingMore] = useState(false);
  const [tagsError, setTagsError] = useState("");

  const [videos, setVideos] = useState<VideoCard[]>([]);
  const [videosNextCursor, setVideosNextCursor] = useState("");
  const [videosLoading, setVideosLoading] = useState(false);
  const [videosLoadingMore, setVideosLoadingMore] = useState(false);
  const [videosError, setVideosError] = useState("");

  const updateSelectedTag = (tag: string) => {
    const params = new URLSearchParams(searchParams.toString());
    if (tag) {
      params.set("tag", tag);
    } else {
      params.delete("tag");
    }
    const next = params.toString();
    router.push(next ? `${pathname}?${next}` : pathname);
  };

  const fetchTags = useCallback(
    async ({ cursor = "", append = false }: { cursor?: string; append?: boolean } = {}) => {
      if (append) {
        setTagsLoadingMore(true);
      } else {
        setTagsLoading(true);
        setTagsError("");
      }

      try {
        const params = new URLSearchParams();
        params.set("limit", String(TAGS_LIMIT));
        if (cursor) {
          params.set("cursor", cursor);
        }
        const data = await request<TagsData>(`/tags?${params.toString()}`, { auth: false });
        const mapped = mapTagsData(data);
        setTags((prev) => {
          if (!append) {
            return mapped.items;
          }
          const seen = new Set(prev.map((item) => item.name));
          const merged = [...prev];
          for (const item of mapped.items) {
            if (seen.has(item.name)) {
              continue;
            }
            seen.add(item.name);
            merged.push(item);
          }
          return merged;
        });
        setTagsNextCursor(mapped.next_cursor ?? "");
      } catch (err) {
        if (!append) {
          setTags([]);
        }
        setTagsError(err instanceof Error ? err.message : "标签加载失败");
      } finally {
        if (append) {
          setTagsLoadingMore(false);
        } else {
          setTagsLoading(false);
        }
      }
    },
    [request],
  );

  const fetchVideosByTag = useCallback(
    async (tag: string, { cursor = "", append = false }: { cursor?: string; append?: boolean } = {}) => {
      if (!tag) {
        setVideos([]);
        setVideosNextCursor("");
        setVideosError("");
        setVideosLoading(false);
        setVideosLoadingMore(false);
        return;
      }

      if (append) {
        setVideosLoadingMore(true);
      } else {
        setVideosLoading(true);
        setVideosError("");
      }

      try {
        const params = new URLSearchParams();
        params.set("tag", tag);
        params.set("limit", String(VIDEOS_LIMIT));
        if (cursor) {
          params.set("cursor", cursor);
        }
        const data = await request<{ items: VideoCard[]; next_cursor?: string }>(`/videos?${params.toString()}`, {
          auth: false,
        });
        const mapped = (data.items ?? []).map(mapVideoCard);
        setVideos((prev) => (append ? [...prev, ...mapped] : mapped));
        setVideosNextCursor(data.next_cursor ?? "");
      } catch (err) {
        if (!append) {
          setVideos([]);
        }
        setVideosError(err instanceof Error ? err.message : "标签视频加载失败");
      } finally {
        if (append) {
          setVideosLoadingMore(false);
        } else {
          setVideosLoading(false);
        }
      }
    },
    [request],
  );

  useEffect(() => {
    void fetchTags();
  }, [fetchTags]);

  useEffect(() => {
    if (!selectedTag) {
      setVideos([]);
      setVideosNextCursor("");
      setVideosError("");
      setVideosLoading(false);
      setVideosLoadingMore(false);
      return;
    }
    void fetchVideosByTag(selectedTag);
  }, [fetchVideosByTag, selectedTag]);

  useEffect(() => {
    if (selectedTag || tagsLoading || tags.length === 0) {
      return;
    }
    updateSelectedTag(tags[0].name);
  }, [selectedTag, tags, tagsLoading]);

  const loadMoreTags = async () => {
    if (!tagsNextCursor || tagsLoadingMore) {
      return;
    }
    await fetchTags({ cursor: tagsNextCursor, append: true });
  };

  const loadMoreVideos = async () => {
    if (!selectedTag || !videosNextCursor || videosLoadingMore) {
      return;
    }
    await fetchVideosByTag(selectedTag, { cursor: videosNextCursor, append: true });
  };

  const selectedTagInfo = useMemo(() => tags.find((item) => item.name === selectedTag), [selectedTag, tags]);

  return (
    <div className="space-y-8">
      <section className="rounded-2xl border border-primary/10 bg-white p-5 shadow-sm md:p-6">
        <div className="mb-4 flex items-center justify-between">
          <h1 className="text-2xl font-bold text-slate-900">标签广场</h1>
          <span className="text-sm text-slate-500">{formatCount(tags.length)} 个标签</span>
        </div>

        {tagsLoading ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {Array.from({ length: 12 }).map((_, idx) => (
              <LoadingSkeleton key={idx} className="h-10 rounded-full" />
            ))}
          </div>
        ) : tagsError ? (
          <EmptyState title="标签加载失败" description={tagsError} />
        ) : tags.length === 0 ? (
          <EmptyState title="暂无标签" description="当前还没有可展示的标签。" />
        ) : (
          <>
            <div className="flex flex-wrap gap-2">
              {tags.map((item) => {
                const active = item.name === selectedTag;
                return (
                  <button
                    key={item.name}
                    type="button"
                    onClick={() => updateSelectedTag(item.name)}
                    className={cn(
                      "rounded-full border px-3 py-1.5 text-xs transition-all",
                      active
                        ? "border-primary bg-primary text-white"
                        : "border-slate-200 bg-white text-slate-700 hover:border-primary/40 hover:text-primary",
                    )}
                  >
                    #{item.name} · {formatCount(item.videos_count)}
                  </button>
                );
              })}
            </div>
            {tagsNextCursor ? (
              <div className="mt-4 flex justify-center">
                <button
                  type="button"
                  onClick={() => void loadMoreTags()}
                  disabled={tagsLoadingMore}
                  className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition hover:border-primary/40 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {tagsLoadingMore ? "加载中..." : "加载更多标签"}
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>

      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-bold text-slate-900">
            {selectedTag ? `#${selectedTag}` : "请选择标签"}
          </h2>
          {selectedTagInfo ? <span className="text-sm text-slate-500">{formatCount(selectedTagInfo.videos_count)} 个视频</span> : null}
        </div>

        {!selectedTag ? (
          <EmptyState title="请先选择一个标签" description="点击上方标签后即可查看对应视频列表。" />
        ) : videosLoading ? (
          <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {Array.from({ length: 8 }).map((_, idx) => (
              <LoadingSkeleton key={idx} className="aspect-video rounded-xl" />
            ))}
          </div>
        ) : videosError ? (
          <EmptyState title="标签视频加载失败" description={videosError} />
        ) : videos.length === 0 ? (
          <EmptyState title="该标签暂无视频" description="试试切换其他标签。" />
        ) : (
          <>
            <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {videos.map((video) => (
                <TagsVideoCard key={video.id} video={video} />
              ))}
            </div>
            {videosNextCursor ? (
              <div className="flex justify-center pt-2">
                <button
                  type="button"
                  onClick={() => void loadMoreVideos()}
                  disabled={videosLoadingMore}
                  className="rounded-xl bg-primary px-5 py-2.5 text-sm font-semibold text-white transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {videosLoadingMore ? "加载中..." : "加载更多视频"}
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>
    </div>
  );
}
