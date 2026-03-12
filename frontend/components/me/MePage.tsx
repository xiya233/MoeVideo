"use client";

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon, type IconName } from "@/components/common/AppIcon";
import { AuthorInline } from "@/components/common/AuthorInline";
import { EmptyState } from "@/components/common/EmptyState";
import type { ContinueWatchingItem, UploadCompleteData, UploadTicket, UserBrief, VideoCard } from "@/lib/dto";
import {
  mapContinueWatchingItem,
  mapUploadCompleteData,
  mapUploadTicket,
  mapUserBrief,
  mapVideoCard,
} from "@/lib/dto/mappers";
import { meApi } from "@/lib/me/api";
import { formatCount } from "@/lib/utils/format";
import { cn } from "@/lib/utils/cn";

const PAGE_LIMIT = 12;
const MAX_AVATAR_SIZE = 10 * 1024 * 1024;
const ALLOWED_AVATAR_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);

type TabKey = "videos" | "continue" | "favorites" | "following" | "edit";

type TabItem = {
  key: TabKey;
  label: string;
  icon: IconName;
};

const TAB_ITEMS: TabItem[] = [
  { key: "videos", label: "我的视频", icon: "play_circle" },
  { key: "continue", label: "继续观看", icon: "autorenew" },
  { key: "favorites", label: "我的收藏", icon: "star" },
  { key: "following", label: "我的关注", icon: "groups" },
  { key: "edit", label: "编辑资料", icon: "edit_note" },
];

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

function videoStatusLabel(status?: string): string {
  if (status === "processing") {
    return "转码中";
  }
  if (status === "failed") {
    return "转码失败";
  }
  if (status === "published") {
    return "已发布";
  }
  return "草稿";
}

function videoStatusClass(status?: string): string {
  if (status === "processing") {
    return "bg-amber-100 text-amber-700";
  }
  if (status === "failed") {
    return "bg-rose-100 text-rose-700";
  }
  if (status === "published") {
    return "bg-emerald-100 text-emerald-700";
  }
  return "bg-slate-100 text-slate-600";
}

function VideoGridCard({ video, showStatus = false }: { video: VideoCard; showStatus?: boolean }) {
  return (
    <article className="group flex flex-col gap-3">
      <Link href={`/videos/${video.id}`} className="relative aspect-video overflow-hidden rounded-xl shadow-md">
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

        {showStatus ? (
          <div className="absolute left-2 top-2">
            <span className={cn("rounded-full px-2 py-1 text-[10px] font-bold", videoStatusClass(video.status))}>
              {videoStatusLabel(video.status)}
            </span>
          </div>
        ) : null}
      </Link>

      <div>
        <h3 className="mb-2 line-clamp-2 text-sm font-bold leading-tight transition-colors group-hover:text-primary">
          {video.title}
        </h3>
        <div className="flex items-center gap-2 opacity-70">
          <AuthorInline username={video.author.username} avatarUrl={video.author.avatar_url} />
          <span className="ml-auto text-[11px]">{video.category || "未分类"}</span>
        </div>
      </div>
    </article>
  );
}

function ContinueWatchingCard({ item }: { item: ContinueWatchingItem }) {
  return (
    <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
      <VideoGridCard video={item.video} />
      <div className="mt-3 space-y-2">
        <div className="flex items-center justify-between text-xs text-slate-500">
          <span>已观看 {formatDurationLabel(item.position_sec)}</span>
          <span>总时长 {formatDurationLabel(item.duration_sec)}</span>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-slate-100">
          <div className="h-full rounded-full bg-primary" style={{ width: `${item.progress_percent}%` }} />
        </div>
      </div>
    </div>
  );
}

function normalizeUserBrief(value: unknown): UserBrief {
  return mapUserBrief(value);
}

export function MePage() {
  const { ready, session, user, openAuthDialog, request, uploadBinary, refreshUser } = useAuth();
  const queryClient = useQueryClient();

  const [activeTab, setActiveTab] = useState<TabKey>("videos");
  const [bioInput, setBioInput] = useState("");
  const [avatarFile, setAvatarFile] = useState<File | null>(null);
  const [avatarPreview, setAvatarPreview] = useState<string>("");
  const [removeAvatar, setRemoveAvatar] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [saveSuccess, setSaveSuccess] = useState("");

  const meQuery = useQuery({
    queryKey: ["me-profile"],
    queryFn: () => request<UserBrief & { email?: string; role?: string }>("/users/me", { auth: true }),
    enabled: !!session,
  });

  const currentUser = meQuery.data ?? user ?? null;

  useEffect(() => {
    setBioInput(currentUser?.bio ?? "");
  }, [currentUser?.bio]);

  useEffect(() => {
    return () => {
      if (avatarPreview) {
        URL.revokeObjectURL(avatarPreview);
      }
    };
  }, [avatarPreview]);

  const videosQuery = useInfiniteQuery({
    queryKey: ["me-videos"],
    initialPageParam: "",
    enabled: !!session && activeTab === "videos",
    queryFn: async ({ pageParam }) => {
      const data = await meApi.listMyVideos(request, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return {
        items: (data.items ?? []).map(mapVideoCard),
        next_cursor: data.next_cursor,
      };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const favoritesQuery = useInfiniteQuery({
    queryKey: ["me-favorites"],
    initialPageParam: "",
    enabled: !!session && activeTab === "favorites",
    queryFn: async ({ pageParam }) => {
      const data = await meApi.listMyFavorites(request, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return {
        items: (data.items ?? []).map(mapVideoCard),
        next_cursor: data.next_cursor,
      };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const continueQuery = useInfiniteQuery({
    queryKey: ["me-continue"],
    initialPageParam: "",
    enabled: !!session && activeTab === "continue",
    queryFn: async ({ pageParam }) => {
      const data = await meApi.listContinueWatching(request, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return {
        items: (data.items ?? []).map(mapContinueWatchingItem),
        next_cursor: data.next_cursor,
      };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const followingQuery = useInfiniteQuery({
    queryKey: ["me-following"],
    initialPageParam: "",
    enabled: !!session && activeTab === "following",
    queryFn: async ({ pageParam }) => {
      const data = await meApi.listMyFollowing(request, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return {
        items: (data.items ?? []).map(normalizeUserBrief),
        next_cursor: data.next_cursor,
      };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const unfollowMutation = useMutation({
    mutationFn: async (targetUserID: string) => {
      await request<{ active: boolean }>(`/users/${targetUserID}/follow`, {
        method: "PUT",
        auth: true,
        body: { active: false },
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me-following"] });
      await refreshUser();
      await meQuery.refetch();
    },
  });

  const saveProfileMutation = useMutation({
    mutationFn: async () => {
      setSaveError("");
      setSaveSuccess("");
      if (!currentUser) {
        throw new Error("用户未登录");
      }

      let nextAvatarMediaID: string | undefined;
      if (avatarFile) {
        if (!ALLOWED_AVATAR_TYPES.has(avatarFile.type)) {
          throw new Error("头像仅支持 JPG/PNG/WEBP");
        }
        if (avatarFile.size <= 0 || avatarFile.size > MAX_AVATAR_SIZE) {
          throw new Error("头像大小不能超过 10MB");
        }

        const ticketRaw = await request<UploadTicket>("/uploads/presign", {
          method: "POST",
          auth: true,
          body: {
            purpose: "cover",
            filename: avatarFile.name,
            content_type: avatarFile.type,
            file_size_bytes: avatarFile.size,
          },
        });
        const ticket = mapUploadTicket(ticketRaw);
        await uploadBinary(ticket.upload_url, avatarFile, ticket.headers);

        const completedRaw = await request<UploadCompleteData>(`/uploads/${ticket.upload_id}/complete`, {
          method: "POST",
          auth: true,
          body: {
            checksum_sha256: "",
            duration_sec: 0,
            width: 0,
            height: 0,
          },
        });
        const completed = mapUploadCompleteData(completedRaw);
        nextAvatarMediaID = completed.media_object_id;
      }

      const payload: { bio?: string; avatar_media_id?: string } = {};
      const nextBio = bioInput.trim();
      if (nextBio !== (currentUser.bio ?? "")) {
        payload.bio = nextBio;
      }
      if (removeAvatar) {
        payload.avatar_media_id = "";
      }
      if (nextAvatarMediaID) {
        payload.avatar_media_id = nextAvatarMediaID;
      }
      if (!payload.bio && payload.avatar_media_id === undefined) {
        return;
      }

      await meApi.updateProfile(request, payload);
      await refreshUser();
      await meQuery.refetch();
      await queryClient.invalidateQueries({ queryKey: ["me-profile"] });
    },
    onSuccess: () => {
      setSaveSuccess("资料已更新");
      setAvatarFile(null);
      setAvatarPreview("");
      setRemoveAvatar(false);
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "更新资料失败";
      setSaveError(message);
    },
  });

  const myVideos = useMemo(
    () => videosQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [videosQuery.data?.pages],
  );
  const favorites = useMemo(
    () => favoritesQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [favoritesQuery.data?.pages],
  );
  const continueWatching = useMemo(
    () => continueQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [continueQuery.data?.pages],
  );
  const following = useMemo(
    () => followingQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [followingQuery.data?.pages],
  );

  if (!ready) {
    return <div className="rounded-2xl border border-slate-100 bg-white p-8 text-sm text-slate-500">正在加载...</div>;
  }

  if (!session) {
    return (
      <div className="py-10">
        <EmptyState title="请先登录" description="登录后可查看个人中心、收藏和继续观看进度。" />
        <div className="mt-6 flex justify-center">
          <button
            type="button"
            onClick={() => openAuthDialog("login")}
            className="rounded-xl bg-primary px-5 py-2 text-sm font-bold text-white"
          >
            立即登录
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <section className="rounded-2xl border border-slate-100 bg-white p-6 shadow-sm">
        <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
          <div className="flex items-center gap-4">
            <div className="h-20 w-20 overflow-hidden rounded-full border-2 border-primary/20 bg-primary/10">
              {currentUser?.avatar_url && !removeAvatar ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={currentUser.avatar_url} alt={currentUser.username} className="h-full w-full object-cover" />
              ) : (
                <div className="flex h-full w-full items-center justify-center text-2xl font-bold text-primary">
                  {(currentUser?.username ?? "U").slice(0, 1).toUpperCase()}
                </div>
              )}
            </div>
            <div>
              <h1 className="text-2xl font-bold text-slate-900">{currentUser?.username ?? "用户"}</h1>
              <p className="mt-1 max-w-2xl text-sm text-slate-500">{currentUser?.bio || "这个人很神秘，还没有写简介。"}</p>
              <div className="mt-2 flex items-center gap-4 text-xs text-slate-500">
                <span>{formatCount(currentUser?.followers_count ?? 0)} 粉丝</span>
                <span>{formatCount(currentUser?.following_count ?? 0)} 关注</span>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Link
              href="/upload"
              className="rounded-xl bg-primary px-4 py-2 text-sm font-bold text-white transition-opacity hover:opacity-90"
            >
              上传视频
            </Link>
            <button
              type="button"
              onClick={() => setActiveTab("edit")}
              className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
            >
              编辑资料
            </button>
          </div>
        </div>
      </section>

      <section className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
        <div className="flex flex-wrap items-center gap-2">
          {TAB_ITEMS.map((tab) => (
            <button
              key={tab.key}
              type="button"
              onClick={() => setActiveTab(tab.key)}
              className={cn(
                "inline-flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium transition-colors",
                activeTab === tab.key ? "bg-primary text-white" : "bg-slate-100 text-slate-600 hover:text-primary",
              )}
            >
              <AppIcon name={tab.icon} size={16} />
              {tab.label}
            </button>
          ))}
        </div>
      </section>

      {activeTab === "videos" ? (
        <section className="space-y-4">
          {videosQuery.isLoading ? <div className="text-sm text-slate-500">正在加载我的视频...</div> : null}
          {myVideos.length === 0 && !videosQuery.isLoading ? (
            <EmptyState title="还没有上传视频" description="去上传页发布你的第一条视频吧。" />
          ) : (
            <div className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
              {myVideos.map((video) => (
                <VideoGridCard key={video.id} video={video} showStatus />
              ))}
            </div>
          )}
          {videosQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => void videosQuery.fetchNextPage()}
              disabled={videosQuery.isFetchingNextPage}
              className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {videosQuery.isFetchingNextPage ? "加载中..." : "加载更多"}
            </button>
          ) : null}
        </section>
      ) : null}

      {activeTab === "continue" ? (
        <section className="space-y-4">
          {continueQuery.isLoading ? <div className="text-sm text-slate-500">正在加载继续观看...</div> : null}
          {continueWatching.length === 0 && !continueQuery.isLoading ? (
            <EmptyState title="暂无继续观看记录" description="开始播放视频后会自动记录进度。" />
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
              {continueWatching.map((item) => (
                <ContinueWatchingCard key={item.video.id} item={item} />
              ))}
            </div>
          )}
          {continueQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => void continueQuery.fetchNextPage()}
              disabled={continueQuery.isFetchingNextPage}
              className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {continueQuery.isFetchingNextPage ? "加载中..." : "加载更多"}
            </button>
          ) : null}
        </section>
      ) : null}

      {activeTab === "favorites" ? (
        <section className="space-y-4">
          {favoritesQuery.isLoading ? <div className="text-sm text-slate-500">正在加载收藏...</div> : null}
          {favorites.length === 0 && !favoritesQuery.isLoading ? (
            <EmptyState title="暂无收藏" description="去视频页点击收藏，稍后会出现在这里。" />
          ) : (
            <div className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
              {favorites.map((video) => (
                <VideoGridCard key={video.id} video={video} />
              ))}
            </div>
          )}
          {favoritesQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => void favoritesQuery.fetchNextPage()}
              disabled={favoritesQuery.isFetchingNextPage}
              className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {favoritesQuery.isFetchingNextPage ? "加载中..." : "加载更多"}
            </button>
          ) : null}
        </section>
      ) : null}

      {activeTab === "following" ? (
        <section className="space-y-4">
          {followingQuery.isLoading ? <div className="text-sm text-slate-500">正在加载关注列表...</div> : null}
          {following.length === 0 && !followingQuery.isLoading ? (
            <EmptyState title="你还没有关注任何人" description="去播放页点击关注，你喜欢的 UP 会出现在这里。" />
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
              {following.map((item) => (
                <div key={item.id} className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
                  <div className="flex items-center gap-3">
                    <div className="h-12 w-12 overflow-hidden rounded-full bg-primary/10">
                      {item.avatar_url ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img src={item.avatar_url} alt={item.username} className="h-full w-full object-cover" />
                      ) : (
                        <div className="flex h-full w-full items-center justify-center text-sm font-bold text-primary">
                          {item.username.slice(0, 1).toUpperCase()}
                        </div>
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-semibold text-slate-900">{item.username}</p>
                      <p className="truncate text-xs text-slate-500">
                        {formatCount(item.followers_count ?? 0)} 粉丝 · {formatCount(item.following_count ?? 0)} 关注
                      </p>
                    </div>
                  </div>

                  <p className="mt-3 line-clamp-2 text-xs text-slate-500">{item.bio || "这个用户暂未填写简介。"}</p>

                  <div className="mt-4 flex items-center justify-between">
                    <span className="text-xs text-slate-400">UID: {item.id.slice(0, 8)}</span>
                    <button
                      type="button"
                      onClick={() => unfollowMutation.mutate(item.id)}
                      disabled={unfollowMutation.isPending}
                      className="rounded-lg border border-slate-200 px-3 py-1.5 text-xs font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      取消关注
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
          {followingQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => void followingQuery.fetchNextPage()}
              disabled={followingQuery.isFetchingNextPage}
              className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {followingQuery.isFetchingNextPage ? "加载中..." : "加载更多"}
            </button>
          ) : null}
        </section>
      ) : null}

      {activeTab === "edit" ? (
        <section className="rounded-2xl border border-slate-100 bg-white p-6 shadow-sm">
          <h2 className="text-lg font-semibold text-slate-900">编辑资料</h2>
          <p className="mt-1 text-sm text-slate-500">支持更新头像和个人简介。</p>

          <div className="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-2">
            <div>
              <p className="mb-2 text-sm font-medium text-slate-700">头像</p>
              <div className="flex items-center gap-4">
                <div className="h-16 w-16 overflow-hidden rounded-full bg-primary/10">
                  {avatarPreview ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={avatarPreview} alt="头像预览" className="h-full w-full object-cover" />
                  ) : currentUser?.avatar_url && !removeAvatar ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={currentUser.avatar_url} alt={currentUser.username} className="h-full w-full object-cover" />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-sm font-bold text-primary">
                      {(currentUser?.username ?? "U").slice(0, 1).toUpperCase()}
                    </div>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  <label className="cursor-pointer rounded-lg border border-slate-200 px-3 py-1.5 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary">
                    选择头像
                    <input
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      className="hidden"
                      onChange={(event) => {
                        const file = event.target.files?.[0];
                        if (!file) {
                          return;
                        }
                        setAvatarFile(file);
                        setAvatarPreview(URL.createObjectURL(file));
                        setRemoveAvatar(false);
                      }}
                    />
                  </label>
                  <button
                    type="button"
                    onClick={() => {
                      setAvatarFile(null);
                      setAvatarPreview("");
                      setRemoveAvatar(true);
                    }}
                    className="rounded-lg border border-slate-200 px-3 py-1.5 text-sm font-medium text-slate-700 transition-colors hover:border-rose-300 hover:text-rose-600"
                  >
                    移除头像
                  </button>
                </div>
              </div>
              <p className="mt-2 text-xs text-slate-400">支持 JPG/PNG/WEBP，大小不超过 10MB。</p>
            </div>

            <div>
              <p className="mb-2 text-sm font-medium text-slate-700">个人简介</p>
              <textarea
                value={bioInput}
                onChange={(event) => setBioInput(event.target.value)}
                maxLength={500}
                className="h-36 w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary"
                placeholder="写点什么介绍你自己吧..."
              />
              <p className="mt-2 text-right text-xs text-slate-400">{bioInput.length}/500</p>
            </div>
          </div>

          {saveError ? <p className="mt-4 text-sm text-rose-600">{saveError}</p> : null}
          {saveSuccess ? <p className="mt-4 text-sm text-emerald-600">{saveSuccess}</p> : null}

          <div className="mt-6 flex justify-end">
            <button
              type="button"
              onClick={() => saveProfileMutation.mutate()}
              disabled={saveProfileMutation.isPending || meQuery.isLoading}
              className="rounded-xl bg-primary px-5 py-2 text-sm font-bold text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {saveProfileMutation.isPending ? "保存中..." : "保存资料"}
            </button>
          </div>
        </section>
      ) : null}
    </div>
  );
}
