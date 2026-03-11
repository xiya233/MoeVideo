"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon } from "@/components/common/AppIcon";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import { ArtHlsPlayer } from "@/components/video/ArtHlsPlayer";
import type { CommentItem, CommentsData, VideoCard, VideoDetail } from "@/lib/dto";
import { mapCommentsData, mapVideoCard, mapVideoDetail } from "@/lib/dto/mappers";
import { cn } from "@/lib/utils/cn";
import { formatCount, formatDate } from "@/lib/utils/format";

type VideoPageProps = {
  videoId: string;
};

function commentAuthorName(comment: CommentItem): string {
  return comment.user.username || "匿名用户";
}

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

const PROGRESS_KEY_PREFIX = "moevideo.progress.v1:";

function progressStorageKey(videoId: string): string {
  return `${PROGRESS_KEY_PREFIX}${videoId}`;
}

function loadLocalProgress(videoId: string): { position_sec: number; duration_sec: number } {
  if (typeof window === "undefined") {
    return { position_sec: 0, duration_sec: 0 };
  }
  try {
    const raw = window.localStorage.getItem(progressStorageKey(videoId));
    if (!raw) {
      return { position_sec: 0, duration_sec: 0 };
    }
    const parsed = JSON.parse(raw) as { position_sec?: unknown; duration_sec?: unknown };
    const position = Number(parsed.position_sec);
    const duration = Number(parsed.duration_sec);
    return {
      position_sec: Number.isFinite(position) ? Math.max(0, Math.floor(position)) : 0,
      duration_sec: Number.isFinite(duration) ? Math.max(0, Math.floor(duration)) : 0,
    };
  } catch {
    return { position_sec: 0, duration_sec: 0 };
  }
}

function saveLocalProgress(videoId: string, positionSec: number, durationSec: number): void {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(
    progressStorageKey(videoId),
    JSON.stringify({
      position_sec: Math.max(0, Math.floor(positionSec)),
      duration_sec: Math.max(0, Math.floor(durationSec)),
      updated_at: Date.now(),
    }),
  );
}

function clearLocalProgress(videoId: string): void {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.removeItem(progressStorageKey(videoId));
}

function shouldOfferResume(positionSec: number, durationSec: number): boolean {
  if (positionSec <= 30) {
    return false;
  }
  if (durationSec <= 0) {
    return true;
  }
  return durationSec - positionSec > 15;
}

function actionButtonClass(active: boolean): string {
  return cn(
    "flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2",
    active ? "bg-primary text-white shadow-lg shadow-primary/30" : "bg-primary/10 text-primary hover:bg-primary/20",
  );
}

export function VideoPage({ videoId }: VideoPageProps) {
  const { request, user, session, openAuthDialog } = useAuth();

  const [detail, setDetail] = useState<VideoDetail | null>(null);
  const [recommendations, setRecommendations] = useState<VideoCard[]>([]);
  const [comments, setComments] = useState<CommentItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [commentInput, setCommentInput] = useState("");
  const [replyTargetId, setReplyTargetId] = useState<string | null>(null);
  const [replyInput, setReplyInput] = useState("");

  const [pendingLike, setPendingLike] = useState(false);
  const [pendingFavorite, setPendingFavorite] = useState(false);
  const [pendingFollow, setPendingFollow] = useState(false);
  const [pendingComment, setPendingComment] = useState(false);
  const [resumePromptOpen, setResumePromptOpen] = useState(false);
  const [resumePositionSec, setResumePositionSec] = useState(0);
  const [playerStartSec, setPlayerStartSec] = useState(0);

  const hasTrackedView = useRef(false);
  const progressRef = useRef<{ positionSec: number; durationSec: number }>({ positionSec: 0, durationSec: 0 });
  const lastPersistedSecRef = useRef(-1);
  const persistingProgressRef = useRef(false);

  const fetchDetail = useCallback(async (): Promise<VideoDetail> => {
    const data = await request<VideoDetail>(`/videos/${videoId}`, { auth: true });
    return mapVideoDetail(data);
  }, [request, videoId]);

  const fetchComments = useCallback(async () => {
    const data = await request<CommentsData>(`/videos/${videoId}/comments?limit=20`, { auth: false });
    setComments(mapCommentsData(data).items ?? []);
  }, [request, videoId]);

  const fetchRecommendations = useCallback(async () => {
    const data = await request<{ items: VideoCard[] }>(`/videos/${videoId}/recommendations?limit=8`, { auth: false });
    setRecommendations((data.items ?? []).map(mapVideoCard));
  }, [request, videoId]);

  const fetchPublishedExtras = useCallback(async () => {
    await Promise.all([fetchRecommendations(), fetchComments()]);
  }, [fetchComments, fetchRecommendations]);

  const fetchPageData = useCallback(async () => {
    setLoading(true);
    setError("");

    try {
      const nextDetail = await fetchDetail();
      setDetail(nextDetail);
      if (nextDetail.status === "published") {
        await fetchPublishedExtras();
      } else {
        setRecommendations([]);
        setComments([]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载播放页失败");
      setDetail(null);
      setRecommendations([]);
      setComments([]);
    } finally {
      setLoading(false);
    }
  }, [fetchDetail, fetchPublishedExtras]);

  const refreshDetailStatus = useCallback(async () => {
    try {
      const nextDetail = await fetchDetail();
      setDetail(nextDetail);
      if (nextDetail.status === "published") {
        await fetchPublishedExtras();
      }
    } catch {
      // Keep the current UI state; periodic refresh should not break playback page.
    }
  }, [fetchDetail, fetchPublishedExtras]);

  useEffect(() => {
    hasTrackedView.current = false;
    void fetchPageData();
  }, [fetchPageData]);

  useEffect(() => {
    if (!detail || detail.status !== "processing") {
      return;
    }
    const timer = window.setInterval(() => {
      void refreshDetailStatus();
    }, 5000);
    return () => {
      window.clearInterval(timer);
    };
  }, [detail, refreshDetailStatus]);

  useEffect(() => {
    if (!detail || detail.status !== "published" || hasTrackedView.current) {
      return;
    }
    hasTrackedView.current = true;
    void request<{ counted: boolean }>(`/videos/${videoId}/view`, {
      method: "POST",
      auth: false,
      body: {},
    });
  }, [detail, request, videoId]);

  useEffect(() => {
    if (!detail || detail.status !== "published") {
      setResumePromptOpen(false);
      setResumePositionSec(0);
      setPlayerStartSec(0);
      progressRef.current = { positionSec: 0, durationSec: 0 };
      lastPersistedSecRef.current = -1;
      return;
    }

    progressRef.current = { positionSec: 0, durationSec: detail.video.duration_sec || 0 };
    lastPersistedSecRef.current = -1;
    setPlayerStartSec(0);

    const serverProgress = Math.max(0, Math.floor(detail.viewer_progress_sec ?? 0));
    const localProgress = loadLocalProgress(videoId).position_sec;
    const initialProgress = session ? serverProgress : localProgress;

    if (shouldOfferResume(initialProgress, detail.video.duration_sec || 0)) {
      setResumePositionSec(initialProgress);
      setResumePromptOpen(true);
    } else {
      setResumePositionSec(0);
      setResumePromptOpen(false);
    }
  }, [detail?.status, detail?.video.duration_sec, detail?.viewer_progress_sec, session, videoId]);

  const requireAuth = (): boolean => {
    if (session) {
      return true;
    }
    openAuthDialog("login");
    return false;
  };

  const clearStoredProgress = useCallback(async () => {
    if (session) {
      try {
        await request<{ saved: boolean; position_sec: number }>(`/videos/${videoId}/progress`, {
          method: "PUT",
          auth: true,
          body: {
            position_sec: 0,
            duration_sec: detail?.video.duration_sec ?? 0,
            completed: true,
          },
        });
      } catch {
        // Ignore progress cleanup errors.
      }
    } else {
      clearLocalProgress(videoId);
    }
    lastPersistedSecRef.current = 0;
  }, [detail?.video.duration_sec, request, session, videoId]);

  const persistProgress = useCallback(
    async ({ force = false, completed = false }: { force?: boolean; completed?: boolean } = {}) => {
      if (!detail || detail.status !== "published") {
        return;
      }
      if (persistingProgressRef.current && !force) {
        return;
      }

      const positionSec = Math.max(0, Math.floor(progressRef.current.positionSec));
      const durationSec = Math.max(0, Math.floor(progressRef.current.durationSec || detail.video.duration_sec || 0));
      if (!force && positionSec === lastPersistedSecRef.current) {
        return;
      }

      const shouldClear = completed || (durationSec > 0 && positionSec >= durationSec - 15);
      const shouldWrite = positionSec > 0 || shouldClear;
      if (!shouldWrite) {
        return;
      }

      persistingProgressRef.current = true;
      try {
        if (session) {
          const data = await request<{ saved: boolean; position_sec: number }>(`/videos/${videoId}/progress`, {
            method: "PUT",
            auth: true,
            body: {
              position_sec: positionSec,
              duration_sec: durationSec,
              completed: shouldClear,
            },
          });
          lastPersistedSecRef.current = Math.max(0, Math.floor(data.position_sec ?? 0));
        } else if (shouldClear) {
          clearLocalProgress(videoId);
          lastPersistedSecRef.current = 0;
        } else {
          saveLocalProgress(videoId, positionSec, durationSec);
          lastPersistedSecRef.current = positionSec;
        }
      } catch {
        // Ignore progress persistence errors.
      } finally {
        persistingProgressRef.current = false;
      }
    },
    [detail, request, session, videoId],
  );

  useEffect(() => {
    if (!detail || detail.status !== "published") {
      return;
    }
    const timer = window.setInterval(() => {
      void persistProgress();
    }, 5000);
    return () => {
      window.clearInterval(timer);
    };
  }, [detail?.status, persistProgress]);

  useEffect(() => {
    const handleBeforeUnload = () => {
      void persistProgress({ force: true });
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload);
    };
  }, [persistProgress]);

  const toggleLike = async () => {
    if (!detail || detail.status !== "published" || pendingLike) {
      return;
    }
    if (!requireAuth()) {
      return;
    }

    setPendingLike(true);
    const prev = detail;
    const nextActive = !detail.viewer_actions.liked;

    setDetail({
      ...detail,
      viewer_actions: { ...detail.viewer_actions, liked: nextActive },
      stats: {
        ...detail.stats,
        likes_count: Math.max(0, detail.stats.likes_count + (nextActive ? 1 : -1)),
      },
    });

    try {
      await request<{ active: boolean }>(`/videos/${videoId}/like`, {
        method: "PUT",
        body: { active: nextActive },
        auth: true,
      });
    } catch {
      setDetail(prev);
    } finally {
      setPendingLike(false);
    }
  };

  const toggleFavorite = async () => {
    if (!detail || detail.status !== "published" || pendingFavorite) {
      return;
    }
    if (!requireAuth()) {
      return;
    }

    setPendingFavorite(true);
    const prev = detail;
    const nextActive = !detail.viewer_actions.favorited;

    setDetail({
      ...detail,
      viewer_actions: { ...detail.viewer_actions, favorited: nextActive },
      stats: {
        ...detail.stats,
        favorites_count: Math.max(0, detail.stats.favorites_count + (nextActive ? 1 : -1)),
      },
    });

    try {
      await request<{ active: boolean }>(`/videos/${videoId}/favorite`, {
        method: "PUT",
        body: { active: nextActive },
        auth: true,
      });
    } catch {
      setDetail(prev);
    } finally {
      setPendingFavorite(false);
    }
  };

  const toggleFollow = async () => {
    if (!detail || pendingFollow) {
      return;
    }
    if (!requireAuth()) {
      return;
    }

    setPendingFollow(true);
    const prev = detail;
    const nextActive = !detail.viewer_actions.following_uploader;
    const followers = Math.max(0, detail.uploader.followers_count + (nextActive ? 1 : -1));

    setDetail({
      ...detail,
      uploader: {
        ...detail.uploader,
        followed: nextActive,
        followers_count: followers,
      },
      video: {
        ...detail.video,
        author: {
          ...detail.video.author,
          followed: nextActive,
          followers_count: followers,
        },
      },
      viewer_actions: {
        ...detail.viewer_actions,
        following_uploader: nextActive,
      },
    });

    try {
      await request<{ active: boolean }>(`/users/${detail.uploader.id}/follow`, {
        method: "PUT",
        body: { active: nextActive },
        auth: true,
      });
    } catch {
      setDetail(prev);
    } finally {
      setPendingFollow(false);
    }
  };

  const shareVideo = async () => {
    if (!detail || detail.status !== "published") {
      return;
    }

    const prev = detail;
    setDetail({
      ...detail,
      stats: {
        ...detail.stats,
        shares_count: detail.stats.shares_count + 1,
      },
    });

    try {
      await request<{ shared: boolean }>(`/videos/${videoId}/share`, {
        method: "POST",
        auth: false,
        body: {},
      });
    } catch {
      setDetail(prev);
    }
  };

  const publishComment = async (payload: { content: string; parent_comment_id?: string }) => {
    if (!detail || detail.status !== "published" || pendingComment) {
      return;
    }
    if (!requireAuth()) {
      return;
    }

    const content = payload.content.trim();
    if (!content) {
      return;
    }

    const snapshot = comments;
    setPendingComment(true);

    const tempId = `temp-${Date.now()}`;
    const optimisticComment: CommentItem = {
      id: tempId,
      video_id: videoId,
      content,
      like_count: 0,
      created_at: new Date().toISOString(),
      parent_comment_id: payload.parent_comment_id ?? null,
      user: {
        id: user?.id ?? "",
        username: user?.username ?? "我",
        avatar_url: user?.avatar_url,
        bio: user?.bio,
      },
      replies: [],
    };

    if (payload.parent_comment_id) {
      setComments((prev) =>
        prev.map((item) =>
          item.id === payload.parent_comment_id ? { ...item, replies: [...item.replies, optimisticComment] } : item,
        ),
      );
    } else {
      setComments((prev) => [optimisticComment, ...prev]);
    }

    try {
      await request<{ id: string }>(`/videos/${videoId}/comments`, {
        method: "POST",
        auth: true,
        body: {
          content,
          parent_comment_id: payload.parent_comment_id,
        },
      });
      if (payload.parent_comment_id) {
        setReplyInput("");
        setReplyTargetId(null);
      } else {
        setCommentInput("");
      }
      await fetchComments();
      setDetail((prev) =>
        prev
          ? {
              ...prev,
              stats: { ...prev.stats, comments_count: prev.stats.comments_count + 1 },
              video: { ...prev.video, comments_count: prev.video.comments_count + 1 },
            }
          : prev,
      );
    } catch {
      setComments(snapshot);
    } finally {
      setPendingComment(false);
    }
  };

  const recommendationCards = useMemo(() => recommendations.slice(0, 8), [recommendations]);
  const playerSource = useMemo(() => {
    if (!detail) {
      return null;
    }
    if (detail.playback.type === "hls" && detail.playback.hls_master_url) {
      return { type: "m3u8" as const, url: detail.playback.hls_master_url };
    }
    const mp4 = detail.playback.mp4_url || detail.source_url;
    if (mp4) {
      return { type: "mp4" as const, url: mp4 };
    }
    return null;
  }, [detail]);
  const playerQualities = useMemo(() => {
    if (!detail || detail.playback.type !== "hls" || !detail.playback.hls_master_url) {
      return [];
    }
    const variants = [...detail.playback.variants]
      .filter((item) => item.url)
      .sort((a, b) => b.height - a.height)
      .map((item) => ({
        html: item.name || `${item.height}p`,
        url: item.url,
        default: false,
      }));
    return [{ html: "Auto", url: detail.playback.hls_master_url, default: true }, ...variants];
  }, [detail]);

  const handlePlayerTimeUpdate = useCallback((positionSec: number, durationSec: number) => {
    progressRef.current = {
      positionSec,
      durationSec,
    };
  }, []);

  const handlePlayerPause = useCallback(
    (positionSec: number, durationSec: number) => {
      progressRef.current = {
        positionSec,
        durationSec,
      };
      void persistProgress({ force: true });
    },
    [persistProgress],
  );

  const handlePlayerEnded = useCallback(
    (durationSec: number) => {
      progressRef.current = {
        positionSec: durationSec,
        durationSec,
      };
      setResumePromptOpen(false);
      setResumePositionSec(0);
      void persistProgress({ force: true, completed: true });
    },
    [persistProgress],
  );

  const continueFromResume = () => {
    setPlayerStartSec(resumePositionSec);
    setResumePromptOpen(false);
  };

  const restartFromBeginning = () => {
    setPlayerStartSec(0);
    setResumePromptOpen(false);
    setResumePositionSec(0);
    progressRef.current = { positionSec: 0, durationSec: detail?.video.duration_sec ?? 0 };
    void clearStoredProgress();
  };

  if (loading) {
    return (
      <div className="flex flex-col gap-8 lg:flex-row">
        <div className="flex-1 space-y-4">
          <LoadingSkeleton className="aspect-video" />
          <LoadingSkeleton className="h-20" />
          <LoadingSkeleton className="h-44" />
        </div>
        <div className="w-full space-y-4 lg:w-96">
          <LoadingSkeleton className="h-24" />
          <LoadingSkeleton className="h-24" />
          <LoadingSkeleton className="h-24" />
        </div>
      </div>
    );
  }

  if (error || !detail) {
    return <EmptyState title="播放页加载失败" description={error || "视频不存在或已下线"} />;
  }

  return (
    <div className="flex flex-col gap-8 lg:flex-row">
      <div className="flex-1 space-y-6">
        <section className="group relative aspect-video overflow-hidden rounded-xl bg-black shadow-2xl">
          {detail.status === "processing" ? (
            <div className="flex h-full w-full items-center justify-center bg-slate-950/90">
              <div className="flex flex-col items-center gap-3 text-slate-200">
                <AppIcon name="autorenew" size={36} className="animate-spin" />
                <p className="text-sm font-semibold">视频正在转码中，页面会自动刷新</p>
              </div>
            </div>
          ) : detail.status === "failed" ? (
            <div className="flex h-full w-full items-center justify-center bg-slate-950/90">
              <div className="flex flex-col items-center gap-2 text-slate-200">
                <AppIcon name="error" size={36} className="text-red-400" />
                <p className="text-sm font-semibold">转码失败，请重新上传视频</p>
              </div>
            </div>
          ) : playerSource ? (
            <ArtHlsPlayer
              sourceType={playerSource.type}
              sourceUrl={playerSource.url}
              poster={detail.video.cover_url}
              qualities={playerQualities}
              startTimeSec={playerStartSec}
              onTimeUpdate={handlePlayerTimeUpdate}
              onPause={handlePlayerPause}
              onEnded={handlePlayerEnded}
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-slate-950/90 text-slate-200">
              暂无可播放视频源
            </div>
          )}
          {detail.status === "published" && resumePromptOpen ? (
            <div className="absolute left-4 top-4 z-10 max-w-[280px] rounded-xl border border-white/20 bg-black/65 p-3 text-white shadow-xl backdrop-blur">
              <p className="text-xs font-semibold">检测到上次播放进度</p>
              <p className="mt-1 text-xs text-white/80">继续播放到 {formatDurationLabel(resumePositionSec)}</p>
              <div className="mt-3 flex items-center gap-2">
                <button
                  type="button"
                  onClick={continueFromResume}
                  className="rounded-lg bg-white px-3 py-1.5 text-xs font-semibold text-slate-900"
                >
                  继续播放
                </button>
                <button
                  type="button"
                  onClick={restartFromBeginning}
                  className="rounded-lg border border-white/40 px-3 py-1.5 text-xs font-semibold text-white"
                >
                  从头开始
                </button>
              </div>
            </div>
          ) : null}
        </section>

        <section className="space-y-4">
          <h1 className="text-2xl font-bold leading-tight md:text-3xl">{detail.video.title}</h1>
          <div className="flex flex-wrap items-center justify-between gap-4 border-b border-primary/10 py-2">
            <div className="flex items-center gap-4 text-sm text-slate-500">
              <span className="flex items-center gap-1">
                <AppIcon name="visibility" size={18} />
                {formatCount(detail.stats.views_count)} 播放
              </span>
              <span className="flex items-center gap-1">
                <AppIcon name="calendar_today" size={18} />
                {formatDate(detail.video.published_at)}
              </span>
            </div>

            {detail.status === "published" ? (
              <div className="flex items-center gap-2">
                <button type="button" onClick={toggleLike} className={actionButtonClass(detail.viewer_actions.liked)} disabled={pendingLike}>
                  <AppIcon name="thumb_up" />
                  {formatCount(detail.stats.likes_count)}
                </button>
                <button
                  type="button"
                  onClick={toggleFavorite}
                  className={actionButtonClass(detail.viewer_actions.favorited)}
                  disabled={pendingFavorite}
                >
                  <AppIcon name="star" />
                  {formatCount(detail.stats.favorites_count)}
                </button>
                <button
                  type="button"
                  onClick={shareVideo}
                  className="flex items-center gap-2 rounded-xl bg-primary/10 px-4 py-2 text-sm font-medium text-primary transition-all hover:bg-primary/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2"
                >
                  <AppIcon name="share" />
                  分享
                </button>
              </div>
            ) : (
              <span className="rounded-full bg-primary/10 px-3 py-1 text-xs font-semibold text-primary">
                {detail.status === "processing" ? "转码处理中" : "转码失败"}
              </span>
            )}
          </div>
        </section>

        <section className="flex items-center justify-between rounded-xl border border-primary/10 bg-primary/5 p-4">
          <div className="flex items-center gap-4">
            <div className="relative">
              <div className="h-14 w-14 overflow-hidden rounded-full border-2 border-primary">
                {detail.uploader.avatar_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={detail.uploader.avatar_url} alt={detail.uploader.username} className="h-full w-full object-cover" />
                ) : (
                  <div className="flex h-full w-full items-center justify-center bg-primary/20 text-sm font-bold text-primary">
                    {detail.uploader.username.slice(0, 1).toUpperCase()}
                  </div>
                )}
              </div>
              <div className="absolute -bottom-1 -right-1 rounded-full border-2 border-white bg-yellow-400 p-0.5 text-white">
                <AppIcon name="verified" size={10} className="block" />
              </div>
            </div>
            <div>
              <h3 className="text-lg font-bold">{detail.uploader.username}</h3>
              <p className="text-sm text-slate-500">{formatCount(detail.uploader.followers_count)} 粉丝</p>
            </div>
          </div>

          <button
            type="button"
            onClick={toggleFollow}
            disabled={pendingFollow}
            className={cn(
              "rounded-xl px-6 py-2 text-sm font-bold transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2",
              detail.viewer_actions.following_uploader
                ? "border border-slate-200 bg-white text-slate-700 hover:border-primary/30 hover:text-primary"
                : "bg-primary text-white hover:shadow-lg hover:shadow-primary/30",
            )}
          >
            {detail.viewer_actions.following_uploader ? "已关注" : "+ 关注"}
          </button>
        </section>

        <section className="rounded-xl bg-primary/5 p-4">
          <p className="whitespace-pre-line text-sm leading-relaxed text-slate-700">{detail.description || "暂无简介"}</p>
          {detail.tags.length > 0 ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {detail.tags.map((tag) => (
                <span key={tag} className="rounded-full bg-primary/10 px-3 py-1 text-xs font-bold text-primary">
                  #{tag}
                </span>
              ))}
            </div>
          ) : null}
        </section>

        {detail.status === "published" ? (
          <section className="space-y-6">
            <h3 className="flex items-center gap-2 text-xl font-bold">
              评论 <span className="text-sm font-normal text-slate-400">{formatCount(detail.stats.comments_count)}</span>
            </h3>

            <div className="flex gap-4">
              <div className="h-10 w-10 flex-shrink-0 overflow-hidden rounded-full bg-slate-200">
                {user?.avatar_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={user.avatar_url} alt={user.username} className="h-full w-full object-cover" />
                ) : null}
              </div>
              <div className="flex-1 space-y-2">
                <textarea
                  className="w-full rounded-xl border border-primary/10 bg-primary/5 p-3 text-sm placeholder:text-slate-400 focus:border-primary focus:ring-primary"
                  placeholder="发一条友善的评论吧..."
                  rows={3}
                  value={commentInput}
                  onChange={(event) => setCommentInput(event.target.value)}
                />
                <div className="flex justify-end">
                  <button
                    type="button"
                    onClick={() => publishComment({ content: commentInput })}
                    disabled={pendingComment}
                    className="rounded-lg bg-primary px-6 py-2 text-sm font-bold text-white transition-all hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {pendingComment ? "发布中..." : "发布评论"}
                  </button>
                </div>
              </div>
            </div>

            <div className="space-y-6">
              {comments.map((item) => (
                <div key={item.id} className="space-y-3">
                  <div className="flex gap-4">
                    <div className="h-10 w-10 overflow-hidden rounded-full bg-slate-200">
                      {item.user.avatar_url ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img src={item.user.avatar_url} alt={commentAuthorName(item)} className="h-full w-full object-cover" />
                      ) : null}
                    </div>
                    <div className="flex-1 space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-bold text-primary">{commentAuthorName(item)}</span>
                        <span className="text-xs text-slate-400">{formatDate(item.created_at)}</span>
                      </div>
                      <p className="text-sm text-slate-800">{item.content}</p>
                      <div className="flex items-center gap-4 pt-2">
                        <button className="flex items-center gap-1 text-xs text-slate-400 transition-colors hover:text-primary" type="button">
                          <AppIcon name="thumb_up" size={14} />
                          {formatCount(item.like_count)}
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            setReplyTargetId(replyTargetId === item.id ? null : item.id);
                            setReplyInput("");
                          }}
                          className="text-xs text-slate-400 transition-colors hover:text-primary"
                        >
                          回复
                        </button>
                      </div>
                    </div>
                  </div>

                  {replyTargetId === item.id ? (
                    <div className="ml-[56px] rounded-xl border border-primary/10 bg-primary/5 p-3">
                      <textarea
                        rows={2}
                        value={replyInput}
                        onChange={(event) => setReplyInput(event.target.value)}
                        placeholder={`回复 @${commentAuthorName(item)}`}
                        className="w-full rounded-lg border border-primary/10 bg-white p-2 text-sm focus:border-primary focus:ring-primary"
                      />
                      <div className="mt-2 flex justify-end gap-2">
                        <button
                          type="button"
                          className="rounded-lg border border-slate-200 bg-white px-3 py-1 text-xs font-semibold text-slate-600"
                          onClick={() => {
                            setReplyTargetId(null);
                            setReplyInput("");
                          }}
                        >
                          取消
                        </button>
                        <button
                          type="button"
                          className="rounded-lg bg-primary px-3 py-1 text-xs font-semibold text-white"
                          onClick={() => publishComment({ content: replyInput, parent_comment_id: item.id })}
                          disabled={pendingComment}
                        >
                          回复
                        </button>
                      </div>
                    </div>
                  ) : null}

                  {item.replies.length > 0 ? (
                    <div className="ml-[56px] space-y-3 rounded-xl border border-slate-100 bg-white p-3">
                      {item.replies.map((reply) => (
                        <div key={reply.id} className="flex gap-3">
                          <div className="h-8 w-8 overflow-hidden rounded-full bg-slate-200">
                            {reply.user.avatar_url ? (
                              // eslint-disable-next-line @next/next/no-img-element
                              <img src={reply.user.avatar_url} alt={commentAuthorName(reply)} className="h-full w-full object-cover" />
                            ) : null}
                          </div>
                          <div className="flex-1">
                            <div className="flex items-center gap-2">
                              <span className="text-xs font-bold text-slate-700">{commentAuthorName(reply)}</span>
                              <span className="text-xs text-slate-400">{formatDate(reply.created_at)}</span>
                            </div>
                            <p className="text-sm text-slate-700">{reply.content}</p>
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : null}
                </div>
              ))}

              {comments.length === 0 ? <EmptyState title="还没有评论" description="成为第一个评论的人吧。" /> : null}
            </div>
          </section>
        ) : (
          <section className="rounded-xl border border-primary/10 bg-primary/5 p-6 text-center text-sm text-slate-600">
            {detail.status === "processing" ? "转码完成后即可评论与互动。" : "视频转码失败，评论区暂不可用。"}
          </section>
        )}
      </div>

      <aside className="w-full space-y-4 lg:w-96">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-lg font-bold">推荐视频</h3>
          <button
            type="button"
            onClick={() => void fetchPageData()}
            className="flex cursor-pointer items-center gap-2 text-xs font-medium text-primary transition-colors hover:opacity-80"
          >
            <AppIcon name="autorenew" size={14} />
            换一换
          </button>
        </div>

        <div className="space-y-4">
          {recommendationCards.map((video) => (
            <Link key={video.id} href={`/videos/${video.id}`} className="group flex cursor-pointer gap-3">
              <div className="relative h-24 w-40 flex-shrink-0 overflow-hidden rounded-lg">
                {video.cover_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={video.cover_url} alt={video.title} className="h-full w-full object-cover transition-transform group-hover:scale-110" />
                ) : (
                  <div className="h-full w-full bg-primary/10" />
                )}
                <div className="absolute bottom-1 right-1 rounded-md bg-black/60 px-1.5 py-0.5 text-[10px] font-medium text-white backdrop-blur-sm">
                  {formatDurationLabel(video.duration_sec)}
                </div>
              </div>
              <div className="flex flex-col justify-between py-0.5">
                <h4 className="line-clamp-2 text-sm font-bold leading-snug transition-colors group-hover:text-primary">{video.title}</h4>
                <div className="space-y-0.5">
                  <p className="text-xs text-slate-500">{video.author.username}</p>
                  <p className="flex items-center gap-1 text-[10px] text-slate-400">
                    <AppIcon name="play_circle" size={12} />
                    {formatCount(video.views_count)} 播放
                  </p>
                </div>
              </div>
            </Link>
          ))}

          {detail.status !== "published" ? (
            <EmptyState title="推荐暂不可用" description="当前视频转码中，完成后会显示相关推荐。" />
          ) : recommendationCards.length === 0 ? (
            <EmptyState title="暂无推荐视频" description="稍后再试试看。" />
          ) : null}
        </div>
      </aside>
    </div>
  );
}
