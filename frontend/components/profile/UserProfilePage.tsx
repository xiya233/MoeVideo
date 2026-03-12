"use client";

import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon, type IconName } from "@/components/common/AppIcon";
import { AuthorInline } from "@/components/common/AuthorInline";
import { EmptyState } from "@/components/common/EmptyState";
import type { UserBrief, UserProfileData, VideoCard } from "@/lib/dto";
import { mapUserBrief, mapUserProfileData, mapVideoCard } from "@/lib/dto/mappers";
import { profileApi } from "@/lib/profile/api";
import { cn } from "@/lib/utils/cn";
import { formatCount } from "@/lib/utils/format";

const PAGE_LIMIT = 12;

type TabKey = "videos" | "favorites" | "following" | "followers";
type TabItem = { key: TabKey; label: string; icon: IconName };

const TAB_ITEMS: TabItem[] = [
  { key: "videos", label: "视频", icon: "play_circle" },
  { key: "favorites", label: "收藏", icon: "star" },
  { key: "following", label: "关注", icon: "groups" },
  { key: "followers", label: "粉丝", icon: "person" },
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

function VideoGridCard({ video }: { video: VideoCard }) {
  return (
    <article className="group flex flex-col gap-3">
      <Link href={`/videos/${video.id}`} className="relative aspect-video overflow-hidden rounded-xl shadow-md">
        {video.cover_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={video.cover_url} alt={video.title} className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105" />
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
          <AuthorInline
            username={video.author.username}
            avatarUrl={video.author.avatar_url}
            href={video.author.id ? `/users/${video.author.id}` : undefined}
          />
          <span className="ml-auto text-[11px]">{video.category || "未分类"}</span>
        </div>
      </div>
    </article>
  );
}

function normalizeUserBrief(value: unknown): UserBrief {
  return mapUserBrief(value);
}

export function UserProfilePage({ userId }: { userId: string }) {
  const { request, user, session, openAuthDialog } = useAuth();
  const [activeTab, setActiveTab] = useState<TabKey>("videos");
  const viewerID = user?.id ?? session?.user.id ?? "";
  const isOwner = viewerID !== "" && viewerID === userId;

  const profileQuery = useQuery({
    queryKey: ["user-profile", userId],
    queryFn: async () => mapUserProfileData(await profileApi.getProfile(request, userId)),
    enabled: !!userId,
  });

  const toggleFollowMutation = useMutation({
    mutationFn: async (active: boolean) => {
      await request<{ active: boolean }>(`/users/${userId}/follow`, {
        method: "PUT",
        auth: true,
        body: { active },
      });
    },
    onSuccess: async () => {
      await profileQuery.refetch();
    },
  });

  const profile = profileQuery.data;

  const visibleTabs = useMemo(() => {
    const all = TAB_ITEMS;
    if (!profile) {
      return all;
    }
    if (isOwner) {
      return all;
    }
    return all.filter((item) => {
      if (item.key === "videos") {
        return profile.user.public_videos ?? true;
      }
      if (item.key === "favorites") {
        return profile.user.public_favorites ?? false;
      }
      if (item.key === "following") {
        return profile.user.public_following ?? false;
      }
      if (item.key === "followers") {
        return profile.user.public_followers ?? false;
      }
      return false;
    });
  }, [isOwner, profile]);

  useEffect(() => {
    if (visibleTabs.some((item) => item.key === activeTab)) {
      return;
    }
    if (visibleTabs.length > 0) {
      setActiveTab(visibleTabs[0].key);
    }
  }, [activeTab, visibleTabs]);

  const videosQuery = useInfiniteQuery({
    queryKey: ["user-profile-videos", userId],
    enabled: !!profile && profile.profile_accessible && activeTab === "videos" && visibleTabs.some((tab) => tab.key === "videos"),
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const data = await profileApi.listVideos(request, userId, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return { items: (data.items ?? []).map(mapVideoCard), next_cursor: data.next_cursor };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const favoritesQuery = useInfiniteQuery({
    queryKey: ["user-profile-favorites", userId],
    enabled:
      !!profile &&
      profile.profile_accessible &&
      activeTab === "favorites" &&
      visibleTabs.some((tab) => tab.key === "favorites"),
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const data = await profileApi.listFavorites(request, userId, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return { items: (data.items ?? []).map(mapVideoCard), next_cursor: data.next_cursor };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const followingQuery = useInfiniteQuery({
    queryKey: ["user-profile-following", userId],
    enabled:
      !!profile &&
      profile.profile_accessible &&
      activeTab === "following" &&
      visibleTabs.some((tab) => tab.key === "following"),
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const data = await profileApi.listFollowing(request, userId, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return { items: (data.items ?? []).map(normalizeUserBrief), next_cursor: data.next_cursor };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const followersQuery = useInfiniteQuery({
    queryKey: ["user-profile-followers", userId],
    enabled:
      !!profile &&
      profile.profile_accessible &&
      activeTab === "followers" &&
      visibleTabs.some((tab) => tab.key === "followers"),
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const data = await profileApi.listFollowers(request, userId, {
        cursor: typeof pageParam === "string" && pageParam ? pageParam : undefined,
        limit: PAGE_LIMIT,
      });
      return { items: (data.items ?? []).map(normalizeUserBrief), next_cursor: data.next_cursor };
    },
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  });

  const videos = useMemo(() => videosQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [], [videosQuery.data?.pages]);
  const favorites = useMemo(
    () => favoritesQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [favoritesQuery.data?.pages],
  );
  const following = useMemo(
    () => followingQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [followingQuery.data?.pages],
  );
  const followers = useMemo(
    () => followersQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [followersQuery.data?.pages],
  );

  if (profileQuery.isLoading) {
    return <div className="rounded-2xl border border-slate-100 bg-white p-8 text-sm text-slate-500">正在加载资料...</div>;
  }

  if (profileQuery.isError || !profile) {
    const message = profileQuery.error instanceof Error ? profileQuery.error.message : "加载资料失败";
    return <EmptyState title="资料加载失败" description={message} />;
  }

  if (!profile.profile_accessible) {
    return (
      <div className="rounded-2xl border border-slate-100 bg-white p-8 shadow-sm">
        <h1 className="text-xl font-bold text-slate-900">{profile.user.username}</h1>
        <p className="mt-2 text-sm text-slate-500">该用户设置了隐私，不允许查看个人资料。</p>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <section className="rounded-2xl border border-slate-100 bg-white p-6 shadow-sm">
        <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
          <div className="flex items-center gap-4">
            <div className="h-20 w-20 overflow-hidden rounded-full border-2 border-primary/20 bg-primary/10">
              {profile.user.avatar_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={profile.user.avatar_url} alt={profile.user.username} className="h-full w-full object-cover" />
              ) : (
                <div className="flex h-full w-full items-center justify-center text-2xl font-bold text-primary">
                  {profile.user.username.slice(0, 1).toUpperCase()}
                </div>
              )}
            </div>
            <div>
              <h1 className="text-2xl font-bold text-slate-900">{profile.user.username}</h1>
              <p className="mt-1 max-w-2xl text-sm text-slate-500">{profile.user.bio || "这个人很神秘，还没有写简介。"}</p>
              <div className="mt-2 flex items-center gap-4 text-xs text-slate-500">
                <span>{formatCount(profile.user.followers_count ?? 0)} 粉丝</span>
                <span>{formatCount(profile.user.following_count ?? 0)} 关注</span>
              </div>
            </div>
          </div>

          {!isOwner ? (
            <button
              type="button"
              onClick={() => {
                if (!session) {
                  openAuthDialog("login");
                  return;
                }
                void toggleFollowMutation.mutate(!profile.followed);
              }}
              disabled={toggleFollowMutation.isPending}
              className={cn(
                "rounded-xl px-5 py-2 text-sm font-bold transition-all",
                profile.followed
                  ? "border border-slate-200 bg-white text-slate-700 hover:border-primary/30 hover:text-primary"
                  : "bg-primary text-white hover:opacity-90",
              )}
            >
              {profile.followed ? "已关注" : "+ 关注"}
            </button>
          ) : null}
        </div>
      </section>

      {visibleTabs.length > 0 ? (
        <section className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
          <div className="flex flex-wrap items-center gap-2">
            {visibleTabs.map((tab) => (
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
      ) : (
        <EmptyState title="暂无公开内容" description="该用户未公开任何分项内容。" />
      )}

      {activeTab === "videos" && visibleTabs.some((tab) => tab.key === "videos") ? (
        <section className="space-y-4">
          {videosQuery.isLoading ? <div className="text-sm text-slate-500">正在加载视频...</div> : null}
          {videos.length === 0 && !videosQuery.isLoading ? (
            <EmptyState title="暂无视频" description="该用户暂未公开视频。" />
          ) : (
            <div className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
              {videos.map((video) => (
                <VideoGridCard key={video.id} video={video} />
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

      {activeTab === "favorites" && visibleTabs.some((tab) => tab.key === "favorites") ? (
        <section className="space-y-4">
          {favoritesQuery.isLoading ? <div className="text-sm text-slate-500">正在加载收藏...</div> : null}
          {favorites.length === 0 && !favoritesQuery.isLoading ? (
            <EmptyState title="暂无收藏" description="该用户暂未公开收藏。" />
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

      {activeTab === "following" && visibleTabs.some((tab) => tab.key === "following") ? (
        <section className="space-y-4">
          {followingQuery.isLoading ? <div className="text-sm text-slate-500">正在加载关注列表...</div> : null}
          {following.length === 0 && !followingQuery.isLoading ? (
            <EmptyState title="暂无关注" description="该用户暂未公开关注列表。" />
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
                      <Link href={`/users/${item.id}`} className="block truncate text-sm font-semibold text-slate-900 hover:text-primary">
                        {item.username}
                      </Link>
                      <p className="truncate text-xs text-slate-500">
                        {formatCount(item.followers_count ?? 0)} 粉丝 · {formatCount(item.following_count ?? 0)} 关注
                      </p>
                    </div>
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

      {activeTab === "followers" && visibleTabs.some((tab) => tab.key === "followers") ? (
        <section className="space-y-4">
          {followersQuery.isLoading ? <div className="text-sm text-slate-500">正在加载粉丝列表...</div> : null}
          {followers.length === 0 && !followersQuery.isLoading ? (
            <EmptyState title="暂无粉丝" description="该用户暂未公开粉丝列表。" />
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
              {followers.map((item) => (
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
                      <Link href={`/users/${item.id}`} className="block truncate text-sm font-semibold text-slate-900 hover:text-primary">
                        {item.username}
                      </Link>
                      <p className="truncate text-xs text-slate-500">
                        {formatCount(item.followers_count ?? 0)} 粉丝 · {formatCount(item.following_count ?? 0)} 关注
                      </p>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
          {followersQuery.hasNextPage ? (
            <button
              type="button"
              onClick={() => void followersQuery.fetchNextPage()}
              disabled={followersQuery.isFetchingNextPage}
              className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              {followersQuery.isFetchingNextPage ? "加载中..." : "加载更多"}
            </button>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}
