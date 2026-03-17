"use client";

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon, type IconName } from "@/components/common/AppIcon";
import { AuthorInline } from "@/components/common/AuthorInline";
import { EmptyState } from "@/components/common/EmptyState";
import { AvatarCropDialog } from "@/components/me/AvatarCropDialog";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import type {
  ContinueWatchingItem,
  UploadCompleteData,
  UploadTicket,
  UserBrief,
  UserYTDLPCookieProfile,
  VideoCard,
} from "@/lib/dto";
import {
  mapContinueWatchingItem,
  mapUserYTDLPCookieProfile,
  mapVideoDetail,
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
const ALLOWED_AVATAR_INPUT_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);

type TabKey = "videos" | "continue" | "favorites" | "following" | "followers" | "edit";

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
  { key: "followers", label: "我的粉丝", icon: "person" },
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

type VideoVisibility = "public" | "unlisted" | "private";

function normalizeVideoVisibility(value: string | undefined): VideoVisibility {
  if (value === "private") {
    return "private";
  }
  if (value === "unlisted") {
    return "unlisted";
  }
  return "public";
}

function videoVisibilityLabel(value: string | undefined): string {
  const visibility = normalizeVideoVisibility(value);
  if (visibility === "private") {
    return "私密";
  }
  if (visibility === "unlisted") {
    return "非公开";
  }
  return "公开";
}

function videoVisibilityClass(value: string | undefined): string {
  const visibility = normalizeVideoVisibility(value);
  if (visibility === "private") {
    return "bg-rose-100 text-rose-700";
  }
  if (visibility === "unlisted") {
    return "bg-sky-100 text-sky-700";
  }
  return "bg-emerald-100 text-emerald-700";
}

function parseVideoTagsInput(input: string): string[] {
  const seen = new Set<string>();
  const tags: string[] = [];
  for (const raw of input.split(",")) {
    const tag = raw.trim();
    if (!tag || seen.has(tag)) {
      continue;
    }
    seen.add(tag);
    tags.push(tag.slice(0, 32));
  }
  return tags;
}

type YTDLPCookieFormat = "header" | "cookies_txt";

function formatCookieFormatLabel(format: YTDLPCookieFormat): string {
  return format === "cookies_txt" ? "cookies.txt" : "Cookie Header";
}

function VideoGridCard({
  video,
  showStatus = false,
  onEdit,
  onDelete,
  deleting = false,
}: {
  video: VideoCard;
  showStatus?: boolean;
  onEdit?: (video: VideoCard) => void;
  onDelete?: (video: VideoCard) => void;
  deleting?: boolean;
}) {
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
          <>
            <div className="absolute left-2 top-2">
              <span className={cn("rounded-full px-2 py-1 text-[10px] font-bold", videoStatusClass(video.status))}>
                {videoStatusLabel(video.status)}
              </span>
            </div>
            <div className="absolute right-2 top-2">
              <span className={cn("rounded-full px-2 py-1 text-[10px] font-bold", videoVisibilityClass(video.visibility))}>
                {videoVisibilityLabel(video.visibility)}
              </span>
            </div>
          </>
        ) : null}
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
        {onEdit || onDelete ? (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            {onEdit ? (
              <button
                type="button"
                onClick={() => onEdit(video)}
                className="rounded-lg border border-slate-200 px-2.5 py-1 text-xs font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
              >
                编辑视频信息
              </button>
            ) : null}
            {onDelete ? (
              <button
                type="button"
                onClick={() => onDelete(video)}
                disabled={deleting}
                className="rounded-lg border border-rose-200 px-2.5 py-1 text-xs font-medium text-rose-600 transition-colors hover:border-rose-300 hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-60"
              >
                删除视频
              </button>
            ) : null}
          </div>
        ) : null}
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
  const [avatarCropSource, setAvatarCropSource] = useState<string>("");
  const [avatarCropOpen, setAvatarCropOpen] = useState(false);
  const [removeAvatar, setRemoveAvatar] = useState(false);
  const [profilePublicInput, setProfilePublicInput] = useState(true);
  const [publicVideosInput, setPublicVideosInput] = useState(true);
  const [publicFavoritesInput, setPublicFavoritesInput] = useState(false);
  const [publicFollowingInput, setPublicFollowingInput] = useState(false);
  const [publicFollowersInput, setPublicFollowersInput] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [saveSuccess, setSaveSuccess] = useState("");
  const [editingVideo, setEditingVideo] = useState<VideoCard | null>(null);
  const [editVideoTitle, setEditVideoTitle] = useState("");
  const [editVideoDescription, setEditVideoDescription] = useState("");
  const [editVideoVisibility, setEditVideoVisibility] = useState<VideoVisibility>("public");
  const [editVideoTagsInput, setEditVideoTagsInput] = useState("");
  const [editVideoLoading, setEditVideoLoading] = useState(false);
  const [editVideoError, setEditVideoError] = useState("");
  const [deletingVideo, setDeletingVideo] = useState<VideoCard | null>(null);
  const [deleteVideoError, setDeleteVideoError] = useState("");
  const [videoActionMessage, setVideoActionMessage] = useState("");
  const [cookieDialogOpen, setCookieDialogOpen] = useState(false);
  const [editingCookie, setEditingCookie] = useState<UserYTDLPCookieProfile | null>(null);
  const [cookieLabelInput, setCookieLabelInput] = useState("");
  const [cookieDomainRuleInput, setCookieDomainRuleInput] = useState("");
  const [cookieFormatInput, setCookieFormatInput] = useState<YTDLPCookieFormat>("header");
  const [cookieContentInput, setCookieContentInput] = useState("");
  const [cookieFormError, setCookieFormError] = useState("");
  const [cookieActionMessage, setCookieActionMessage] = useState("");

  const meQuery = useQuery({
    queryKey: ["me-profile"],
    queryFn: () => request<UserBrief & { email?: string; role?: string }>("/users/me", { auth: true }),
    enabled: !!session,
  });

  const currentUser = meQuery.data ?? user ?? null;

  const ytdlpCookiesQuery = useQuery({
    queryKey: ["me-ytdlp-cookies"],
    queryFn: async () => {
      const data = await meApi.listMyYTDLPCookies(request);
      return (data.items ?? []).map(mapUserYTDLPCookieProfile);
    },
    enabled: !!session && activeTab === "edit",
  });

  useEffect(() => {
    setBioInput(currentUser?.bio ?? "");
    setProfilePublicInput(currentUser?.profile_public ?? true);
    setPublicVideosInput(currentUser?.public_videos ?? true);
    setPublicFavoritesInput(currentUser?.public_favorites ?? false);
    setPublicFollowingInput(currentUser?.public_following ?? false);
    setPublicFollowersInput(currentUser?.public_followers ?? false);
  }, [currentUser]);

  useEffect(() => {
    return () => {
      if (avatarPreview) {
        URL.revokeObjectURL(avatarPreview);
      }
    };
  }, [avatarPreview]);

  useEffect(() => {
    return () => {
      if (avatarCropSource) {
        URL.revokeObjectURL(avatarCropSource);
      }
    };
  }, [avatarCropSource]);

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

  const followersQuery = useInfiniteQuery({
    queryKey: ["me-followers"],
    initialPageParam: "",
    enabled: !!session && activeTab === "followers",
    queryFn: async ({ pageParam }) => {
      const data = await meApi.listMyFollowers(request, {
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

  const toggleFollowMutation = useMutation({
    mutationFn: async ({ targetUserID, active }: { targetUserID: string; active: boolean }) => {
      await request<{ active: boolean }>(`/users/${targetUserID}/follow`, {
        method: "PUT",
        auth: true,
        body: { active },
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me-following"] });
      await queryClient.invalidateQueries({ queryKey: ["me-followers"] });
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
        if (avatarFile.type !== "image/webp") {
          throw new Error("头像请先裁剪后上传（WebP）");
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

      const payload: {
        bio?: string;
        avatar_media_id?: string;
        profile_public?: boolean;
        public_videos?: boolean;
        public_favorites?: boolean;
        public_following?: boolean;
        public_followers?: boolean;
      } = {};
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
      if (profilePublicInput !== (currentUser.profile_public ?? true)) {
        payload.profile_public = profilePublicInput;
      }
      if (publicVideosInput !== (currentUser.public_videos ?? true)) {
        payload.public_videos = publicVideosInput;
      }
      if (publicFavoritesInput !== (currentUser.public_favorites ?? false)) {
        payload.public_favorites = publicFavoritesInput;
      }
      if (publicFollowingInput !== (currentUser.public_following ?? false)) {
        payload.public_following = publicFollowingInput;
      }
      if (publicFollowersInput !== (currentUser.public_followers ?? false)) {
        payload.public_followers = publicFollowersInput;
      }
      if (
        !payload.bio &&
        payload.avatar_media_id === undefined &&
        payload.profile_public === undefined &&
        payload.public_videos === undefined &&
        payload.public_favorites === undefined &&
        payload.public_following === undefined &&
        payload.public_followers === undefined
      ) {
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

  const openVideoEditDialog = useCallback(
    async (video: VideoCard) => {
      setEditingVideo(video);
      setEditVideoTitle(video.title);
      setEditVideoDescription("");
      setEditVideoVisibility(normalizeVideoVisibility(video.visibility));
      setEditVideoTagsInput("");
      setEditVideoError("");
      setEditVideoLoading(true);
      try {
        const detailRaw = await meApi.getMyVideoDetail(request, video.id);
        const detail = mapVideoDetail(detailRaw);
        setEditVideoTitle(detail.video.title || video.title);
        setEditVideoDescription(detail.description || "");
        setEditVideoVisibility(normalizeVideoVisibility(detail.video.visibility || video.visibility));
        setEditVideoTagsInput((detail.tags ?? []).join(", "));
      } catch (error) {
        setEditVideoError(error instanceof Error ? error.message : "加载视频详情失败");
      } finally {
        setEditVideoLoading(false);
      }
    },
    [request],
  );

  const closeVideoEditDialog = useCallback(() => {
    setEditingVideo(null);
    setEditVideoError("");
    setEditVideoLoading(false);
  }, []);

  const openDeleteVideoDialog = useCallback((video: VideoCard) => {
    setDeletingVideo(video);
    setDeleteVideoError("");
    setVideoActionMessage("");
  }, []);

  const closeDeleteVideoDialog = useCallback(() => {
    setDeletingVideo(null);
    setDeleteVideoError("");
  }, []);

  const updateVideoMutation = useMutation({
    mutationFn: async () => {
      if (!editingVideo) {
        return;
      }
      const title = editVideoTitle.trim();
      if (!title) {
        throw new Error("标题不能为空");
      }
      await meApi.updateMyVideo(request, editingVideo.id, {
        title,
        description: editVideoDescription.trim(),
        visibility: editVideoVisibility,
        tags: parseVideoTagsInput(editVideoTagsInput),
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me-videos"] });
      closeVideoEditDialog();
    },
    onError: (error) => {
      setEditVideoError(error instanceof Error ? error.message : "更新视频信息失败");
    },
  });

  const deleteVideoMutation = useMutation({
    mutationFn: async (video: VideoCard) => meApi.deleteMyVideo(request, video.id),
    onSuccess: async (result, video) => {
      await queryClient.invalidateQueries({ queryKey: ["me-videos"] });
      await queryClient.invalidateQueries({ queryKey: ["me-profile"] });
      if (editingVideo?.id === video.id) {
        closeVideoEditDialog();
      }
      closeDeleteVideoDialog();
      if (result.cleanup_warnings && result.cleanup_warnings.length > 0) {
        setVideoActionMessage("视频已删除（部分存储清理失败，请稍后检查日志）");
      } else {
        setVideoActionMessage("视频已删除");
      }
    },
    onError: (error) => {
      setDeleteVideoError(error instanceof Error ? error.message : "删除视频失败");
    },
  });

  const openCookieDialog = useCallback((cookie?: UserYTDLPCookieProfile) => {
    setEditingCookie(cookie ?? null);
    setCookieLabelInput(cookie?.label ?? "");
    setCookieDomainRuleInput(cookie?.domain_rule ?? "");
    setCookieFormatInput(cookie?.format ?? "header");
    setCookieContentInput("");
    setCookieFormError("");
    setCookieDialogOpen(true);
    setCookieActionMessage("");
  }, []);

  const closeCookieDialog = useCallback(() => {
    setCookieDialogOpen(false);
    setEditingCookie(null);
    setCookieContentInput("");
    setCookieFormError("");
  }, []);

  const saveCookieMutation = useMutation({
    mutationFn: async () => {
      const label = cookieLabelInput.trim();
      const domainRule = cookieDomainRuleInput.trim();
      const content = cookieContentInput.trim();
      if (!label) {
        throw new Error("请填写配置名称");
      }
      if (!domainRule) {
        throw new Error("请填写域名规则");
      }
      if (!content) {
        throw new Error("请填写 Cookie 内容");
      }

      if (editingCookie) {
        await meApi.updateMyYTDLPCookie(request, editingCookie.id, {
          label,
          domain_rule: domainRule,
          format: cookieFormatInput,
          content,
        });
      } else {
        await meApi.createMyYTDLPCookie(request, {
          label,
          domain_rule: domainRule,
          format: cookieFormatInput,
          content,
        });
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me-ytdlp-cookies"] });
      setCookieActionMessage(editingCookie ? "Cookie 配置已更新" : "Cookie 配置已新增");
      closeCookieDialog();
    },
    onError: (error) => {
      setCookieFormError(error instanceof Error ? error.message : "保存 Cookie 配置失败");
    },
  });

  const deleteCookieMutation = useMutation({
    mutationFn: async (cookie: UserYTDLPCookieProfile) => meApi.deleteMyYTDLPCookie(request, cookie.id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me-ytdlp-cookies"] });
      setCookieActionMessage("Cookie 配置已删除");
    },
  });

  const onDeleteCookie = useCallback(
    (cookie: UserYTDLPCookieProfile) => {
      if (deleteCookieMutation.isPending) {
        return;
      }
      if (!window.confirm(`确认删除 Cookie 配置「${cookie.label}」吗？`)) {
        return;
      }
      deleteCookieMutation.mutate(cookie);
    },
    [deleteCookieMutation],
  );

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
  const followers = useMemo(
    () => followersQuery.data?.pages.flatMap((page) => page.items ?? []) ?? [],
    [followersQuery.data?.pages],
  );
  const ytdlpCookies = useMemo(() => ytdlpCookiesQuery.data ?? [], [ytdlpCookiesQuery.data]);

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
          {videoActionMessage ? <p className="text-sm text-emerald-600">{videoActionMessage}</p> : null}
          {videosQuery.isLoading ? <div className="text-sm text-slate-500">正在加载我的视频...</div> : null}
          {myVideos.length === 0 && !videosQuery.isLoading ? (
            <EmptyState title="还没有上传视频" description="去上传页发布你的第一条视频吧。" />
          ) : (
            <div className="grid grid-cols-1 gap-x-6 gap-y-10 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
              {myVideos.map((video) => (
                <VideoGridCard
                  key={video.id}
                  video={video}
                  showStatus
                  onEdit={(item) => void openVideoEditDialog(item)}
                  onDelete={openDeleteVideoDialog}
                  deleting={deleteVideoMutation.isPending && deletingVideo?.id === video.id}
                />
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
                      <Link href={`/users/${item.id}`} className="block truncate text-sm font-semibold text-slate-900 hover:text-primary">
                        {item.username}
                      </Link>
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
                      onClick={() => toggleFollowMutation.mutate({ targetUserID: item.id, active: false })}
                      disabled={toggleFollowMutation.isPending}
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

      {activeTab === "followers" ? (
        <section className="space-y-4">
          {followersQuery.isLoading ? <div className="text-sm text-slate-500">正在加载粉丝列表...</div> : null}
          {followers.length === 0 && !followersQuery.isLoading ? (
            <EmptyState title="暂无粉丝" description="继续创作优质内容，粉丝会越来越多。" />
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

                  <p className="mt-3 line-clamp-2 text-xs text-slate-500">{item.bio || "这个用户暂未填写简介。"}</p>

                  <div className="mt-4 flex items-center justify-between">
                    <span className="text-xs text-slate-400">UID: {item.id.slice(0, 8)}</span>
                    <button
                      type="button"
                      onClick={() => toggleFollowMutation.mutate({ targetUserID: item.id, active: !item.followed })}
                      disabled={toggleFollowMutation.isPending}
                      className={cn(
                        "rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50",
                        item.followed
                          ? "border-slate-200 text-slate-700 hover:border-primary/30 hover:text-primary"
                          : "border-primary/30 text-primary hover:bg-primary/10",
                      )}
                    >
                      {item.followed ? "取消关注" : "关注"}
                    </button>
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
                        event.currentTarget.value = "";
                        if (!file) {
                          return;
                        }
                        if (!ALLOWED_AVATAR_INPUT_TYPES.has(file.type)) {
                          setSaveError("头像仅支持 JPG/PNG/WEBP");
                          return;
                        }
                        if (file.size <= 0 || file.size > MAX_AVATAR_SIZE) {
                          setSaveError("头像大小不能超过 10MB");
                          return;
                        }
                        setSaveError("");
                        if (avatarCropSource) {
                          URL.revokeObjectURL(avatarCropSource);
                        }
                        setAvatarCropSource(URL.createObjectURL(file));
                        setAvatarCropOpen(true);
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
              <p className="mt-2 text-xs text-slate-400">支持 JPG/PNG/WEBP，裁剪后统一保存为 512x512 WebP。</p>
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

            <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 lg:col-span-2">
              <h3 className="text-sm font-semibold text-slate-800">隐私选项</h3>
              <p className="mt-1 text-xs text-slate-500">你可以控制个人资料页以及分项内容的公开范围。</p>

              <div className="mt-4 space-y-3">
                <label className="flex items-center justify-between rounded-lg bg-white px-3 py-2">
                  <span className="text-sm text-slate-700">公开个人资料</span>
                  <input
                    type="checkbox"
                    checked={profilePublicInput}
                    onChange={(event) => setProfilePublicInput(event.target.checked)}
                    className="h-4 w-4 accent-primary"
                  />
                </label>

                <label className="flex items-center justify-between rounded-lg bg-white px-3 py-2">
                  <span className="text-sm text-slate-700">公开我的视频</span>
                  <input
                    type="checkbox"
                    checked={publicVideosInput}
                    onChange={(event) => setPublicVideosInput(event.target.checked)}
                    disabled={!profilePublicInput}
                    className="h-4 w-4 accent-primary"
                  />
                </label>

                <label className="flex items-center justify-between rounded-lg bg-white px-3 py-2">
                  <span className="text-sm text-slate-700">公开我的收藏</span>
                  <input
                    type="checkbox"
                    checked={publicFavoritesInput}
                    onChange={(event) => setPublicFavoritesInput(event.target.checked)}
                    disabled={!profilePublicInput}
                    className="h-4 w-4 accent-primary"
                  />
                </label>

                <label className="flex items-center justify-between rounded-lg bg-white px-3 py-2">
                  <span className="text-sm text-slate-700">公开我的关注</span>
                  <input
                    type="checkbox"
                    checked={publicFollowingInput}
                    onChange={(event) => setPublicFollowingInput(event.target.checked)}
                    disabled={!profilePublicInput}
                    className="h-4 w-4 accent-primary"
                  />
                </label>

                <label className="flex items-center justify-between rounded-lg bg-white px-3 py-2">
                  <span className="text-sm text-slate-700">公开我的粉丝</span>
                  <input
                    type="checkbox"
                    checked={publicFollowersInput}
                    onChange={(event) => setPublicFollowersInput(event.target.checked)}
                    disabled={!profilePublicInput}
                    className="h-4 w-4 accent-primary"
                  />
                </label>
              </div>
            </div>

            <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 lg:col-span-2">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-slate-800">yt-dlp Cookies</h3>
                  <p className="mt-1 text-xs text-slate-500">
                    配置后可在 URL 导入时手动选择使用。列表不回显明文，仅在新增/编辑时覆盖提交。
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => openCookieDialog()}
                  className="rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
                >
                  新增配置
                </button>
              </div>

              {cookieActionMessage ? <p className="mt-3 text-xs text-emerald-600">{cookieActionMessage}</p> : null}

              {ytdlpCookiesQuery.isLoading ? (
                <p className="mt-3 text-xs text-slate-500">正在加载 Cookie 配置...</p>
              ) : null}
              {ytdlpCookiesQuery.isError ? (
                <p className="mt-3 text-xs text-rose-600">加载 Cookie 配置失败，请稍后重试。</p>
              ) : null}

              {ytdlpCookies.length === 0 && !ytdlpCookiesQuery.isLoading ? (
                <p className="mt-3 text-xs text-slate-500">暂无配置。可按站点域名新增一条配置供 URL 导入手选。</p>
              ) : null}

              {ytdlpCookies.length > 0 ? (
                <div className="mt-3 space-y-2">
                  {ytdlpCookies.map((cookie) => (
                    <div
                      key={cookie.id}
                      className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-slate-800">{cookie.label}</p>
                        <p className="truncate text-xs text-slate-500">
                          {cookie.domain_rule} · {formatCookieFormatLabel(cookie.format)}
                          {cookie.updated_at ? ` · 更新于 ${cookie.updated_at}` : ""}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => openCookieDialog(cookie)}
                          className="rounded-lg border border-slate-200 px-2.5 py-1 text-xs font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
                        >
                          编辑
                        </button>
                        <button
                          type="button"
                          onClick={() => onDeleteCookie(cookie)}
                          disabled={deleteCookieMutation.isPending}
                          className="rounded-lg border border-rose-200 px-2.5 py-1 text-xs font-medium text-rose-600 transition-colors hover:border-rose-300 hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-60"
                        >
                          删除
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              ) : null}
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

      <Dialog open={!!editingVideo} onOpenChange={(open) => (!open ? closeVideoEditDialog() : null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>编辑视频信息</DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">标题</label>
              <input
                value={editVideoTitle}
                onChange={(event) => setEditVideoTitle(event.target.value)}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                placeholder="请输入视频标题"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">描述</label>
              <textarea
                value={editVideoDescription}
                onChange={(event) => setEditVideoDescription(event.target.value)}
                rows={5}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                placeholder="请输入视频描述"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">可见性</label>
              <select
                value={editVideoVisibility}
                onChange={(event) => setEditVideoVisibility(event.target.value as VideoVisibility)}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
              >
                <option value="public">公开（所有人可见）</option>
                <option value="unlisted">非公开列表（仅持链接可访问）</option>
                <option value="private">私密（仅自己可见）</option>
              </select>
            </div>

            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">标签（逗号分隔）</label>
              <input
                value={editVideoTagsInput}
                onChange={(event) => setEditVideoTagsInput(event.target.value)}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                placeholder="例如：动漫,二创"
              />
            </div>

            {editVideoLoading ? <p className="text-xs text-slate-500">正在加载当前视频详情...</p> : null}
            {editVideoError ? <p className="text-xs text-rose-600">{editVideoError}</p> : null}
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => closeVideoEditDialog()}
              className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => updateVideoMutation.mutate()}
              disabled={editVideoLoading || updateVideoMutation.isPending}
              className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {updateVideoMutation.isPending ? "保存中..." : "保存"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!deletingVideo}
        onOpenChange={(open) => (!open && !deleteVideoMutation.isPending ? closeDeleteVideoDialog() : null)}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>删除视频</DialogTitle>
          </DialogHeader>

          <div className="space-y-2 text-sm text-slate-600">
            <p>删除后视频及关联资源将不可恢复，确认继续吗？</p>
            {deletingVideo ? <p className="font-medium text-slate-800">{deletingVideo.title}</p> : null}
            {deleteVideoError ? <p className="text-rose-600">{deleteVideoError}</p> : null}
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={closeDeleteVideoDialog}
              disabled={deleteVideoMutation.isPending}
              className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => {
                if (deletingVideo) {
                  deleteVideoMutation.mutate(deletingVideo);
                }
              }}
              disabled={deleteVideoMutation.isPending || !deletingVideo}
              className="rounded-lg bg-rose-600 px-4 py-2 text-sm font-semibold text-white transition-opacity hover:bg-rose-500 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {deleteVideoMutation.isPending ? "删除中..." : "确认删除"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={cookieDialogOpen}
        onOpenChange={(open) => (!open && !saveCookieMutation.isPending ? closeCookieDialog() : null)}
      >
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{editingCookie ? "编辑 yt-dlp Cookie 配置" : "新增 yt-dlp Cookie 配置"}</DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">配置名称</label>
              <input
                value={cookieLabelInput}
                onChange={(event) => setCookieLabelInput(event.target.value)}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                placeholder="例如：24av 账号 Cookie"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">域名规则</label>
              <input
                value={cookieDomainRuleInput}
                onChange={(event) => setCookieDomainRuleInput(event.target.value)}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                placeholder="例如：24av.net"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">格式</label>
              <select
                value={cookieFormatInput}
                onChange={(event) => setCookieFormatInput(event.target.value as YTDLPCookieFormat)}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
              >
                <option value="header">Cookie Header</option>
                <option value="cookies_txt">cookies.txt</option>
              </select>
            </div>

            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">内容（保存后不回显）</label>
              <textarea
                value={cookieContentInput}
                onChange={(event) => setCookieContentInput(event.target.value)}
                rows={6}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                placeholder={cookieFormatInput === "cookies_txt" ? "粘贴 cookies.txt 内容" : "例如：sessionid=...; token=..."}
              />
            </div>

            {cookieFormError ? <p className="text-xs text-rose-600">{cookieFormError}</p> : null}
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={closeCookieDialog}
              disabled={saveCookieMutation.isPending}
              className="rounded-lg border border-slate-200 px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => saveCookieMutation.mutate()}
              disabled={saveCookieMutation.isPending}
              className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {saveCookieMutation.isPending ? "保存中..." : "保存"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AvatarCropDialog
        open={avatarCropOpen}
        source={avatarCropSource}
        onOpenChange={(open) => {
          setAvatarCropOpen(open);
          if (!open && avatarCropSource) {
            URL.revokeObjectURL(avatarCropSource);
            setAvatarCropSource("");
          }
        }}
        onConfirm={(file, previewURL) => {
          setAvatarFile(file);
          setAvatarPreview((prev) => {
            if (prev) {
              URL.revokeObjectURL(prev);
            }
            return previewURL;
          });
          setRemoveAvatar(false);
        }}
      />
    </div>
  );
}
