"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import type { CommentItem, CommentsData, VideoCard, VideoDetail } from "@/lib/dto";
import { mapCommentsData, mapVideoCard, mapVideoDetail } from "@/lib/dto/mappers";
import { formatCount, formatDate } from "@/lib/utils/format";

type VideoPageProps = {
  videoId: string;
};

function commentAuthorName(comment: CommentItem): string {
  return comment.user.username || "匿名用户";
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

  const hasTrackedView = useRef(false);

  const fetchComments = useCallback(async () => {
    const data = await request<CommentsData>(`/videos/${videoId}/comments?limit=20`, { auth: false });
    setComments(mapCommentsData(data).items ?? []);
  }, [request, videoId]);

  const fetchPageData = useCallback(async () => {
    setLoading(true);
    setError("");

    try {
      const [detailData, recData, commentData] = await Promise.all([
        request<VideoDetail>(`/videos/${videoId}`, { auth: false }),
        request<{ items: VideoCard[] }>(`/videos/${videoId}/recommendations?limit=8`, { auth: false }),
        request<CommentsData>(`/videos/${videoId}/comments?limit=20`, { auth: false }),
      ]);

      setDetail(mapVideoDetail(detailData));
      setRecommendations((recData.items ?? []).map(mapVideoCard));
      setComments(mapCommentsData(commentData).items ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载播放页失败");
      setDetail(null);
      setRecommendations([]);
      setComments([]);
    } finally {
      setLoading(false);
    }
  }, [request, videoId]);

  useEffect(() => {
    void fetchPageData();
  }, [fetchPageData]);

  useEffect(() => {
    if (!detail || hasTrackedView.current) {
      return;
    }
    hasTrackedView.current = true;
    void request<{ counted: boolean }>(`/videos/${videoId}/view`, {
      method: "POST",
      auth: false,
      body: {},
    });
  }, [detail, request, videoId]);

  const requireAuth = (): boolean => {
    if (session) {
      return true;
    }
    openAuthDialog("login");
    return false;
  };

  const toggleLike = async () => {
    if (!detail || pendingLike) {
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
    if (!detail || pendingFavorite) {
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
    if (!detail) {
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
    if (!requireAuth()) {
      return;
    }
    if (!detail || pendingComment) {
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
          item.id === payload.parent_comment_id
            ? { ...item, replies: [...item.replies, optimisticComment] }
            : item,
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

  if (loading) {
    return (
      <div className="grid grid-cols-1 gap-8 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-4">
          <LoadingSkeleton className="aspect-video" />
          <LoadingSkeleton className="h-20" />
          <LoadingSkeleton className="h-40" />
        </div>
        <div className="space-y-3">
          <LoadingSkeleton className="h-20" />
          <LoadingSkeleton className="h-20" />
          <LoadingSkeleton className="h-20" />
        </div>
      </div>
    );
  }

  if (error || !detail) {
    return <EmptyState title="播放页加载失败" description={error || "视频不存在或已下线"} />;
  }

  return (
    <div className="grid grid-cols-1 gap-8 lg:grid-cols-[minmax(0,1fr)_360px]">
      <div className="space-y-6">
        <section className="overflow-hidden rounded-xl bg-black shadow-2xl">
          <div className="relative aspect-video">
            <video
              className="h-full w-full"
              controls
              poster={detail.video.cover_url}
              src={detail.source_url}
              preload="metadata"
            />
          </div>
        </section>

        <section className="space-y-4">
          <h1 className="text-2xl font-bold leading-tight text-slate-900 md:text-3xl">{detail.video.title}</h1>
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-sky-100 pb-3">
            <div className="flex items-center gap-4 text-sm text-slate-500">
              <span className="flex items-center gap-1">
                <span className="material-symbols-outlined text-lg">visibility</span>
                {formatCount(detail.stats.views_count)} 播放
              </span>
              <span className="flex items-center gap-1">
                <span className="material-symbols-outlined text-lg">calendar_today</span>
                {formatDate(detail.video.published_at)}
              </span>
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={toggleLike}
                className={`flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium transition ${
                  detail.viewer_actions.liked
                    ? "bg-sky-500 text-white"
                    : "bg-sky-100 text-sky-700 hover:bg-sky-200"
                }`}
                disabled={pendingLike}
              >
                <span className="material-symbols-outlined">thumb_up</span>
                {formatCount(detail.stats.likes_count)}
              </button>
              <button
                type="button"
                onClick={toggleFavorite}
                className={`flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium transition ${
                  detail.viewer_actions.favorited
                    ? "bg-sky-500 text-white"
                    : "bg-sky-100 text-sky-700 hover:bg-sky-200"
                }`}
                disabled={pendingFavorite}
              >
                <span className="material-symbols-outlined">star</span>
                {formatCount(detail.stats.favorites_count)}
              </button>
              <button
                type="button"
                onClick={shareVideo}
                className="flex items-center gap-2 rounded-xl bg-sky-100 px-4 py-2 text-sm font-medium text-sky-700 transition hover:bg-sky-200"
              >
                <span className="material-symbols-outlined">share</span>
                分享
              </button>
            </div>
          </div>
        </section>

        <section className="flex items-center justify-between rounded-xl border border-sky-100 bg-sky-50/60 p-4">
          <div className="flex items-center gap-3">
            <div className="h-14 w-14 overflow-hidden rounded-full border-2 border-sky-500">
              {detail.uploader.avatar_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={detail.uploader.avatar_url} alt={detail.uploader.username} className="h-full w-full object-cover" />
              ) : (
                <div className="flex h-full w-full items-center justify-center bg-sky-100 text-sm font-bold text-sky-700">
                  {detail.uploader.username.slice(0, 1).toUpperCase()}
                </div>
              )}
            </div>
            <div>
              <h3 className="text-lg font-bold text-slate-900">{detail.uploader.username}</h3>
              <p className="text-sm text-slate-500">{formatCount(detail.uploader.followers_count)} 粉丝</p>
            </div>
          </div>
          <button
            type="button"
            onClick={toggleFollow}
            disabled={pendingFollow}
            className={`rounded-xl px-6 py-2 text-sm font-bold transition ${
              detail.viewer_actions.following_uploader
                ? "border border-slate-300 bg-white text-slate-700 hover:border-sky-300 hover:text-sky-600"
                : "bg-sky-500 text-white hover:bg-sky-600"
            }`}
          >
            {detail.viewer_actions.following_uploader ? "已关注" : "+ 关注"}
          </button>
        </section>

        <section className="rounded-xl bg-sky-50/60 p-4">
          <p className="whitespace-pre-line text-sm leading-relaxed text-slate-700">{detail.description || "暂无简介"}</p>
          {detail.tags.length > 0 ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {detail.tags.map((tag) => (
                <span key={tag} className="rounded-full bg-white px-3 py-1 text-xs font-semibold text-sky-700 ring-1 ring-sky-100">
                  #{tag}
                </span>
              ))}
            </div>
          ) : null}
        </section>

        <section className="space-y-5">
          <h3 className="flex items-center gap-2 text-xl font-bold text-slate-900">
            评论 <span className="text-sm font-normal text-slate-400">{formatCount(detail.stats.comments_count)}</span>
          </h3>

          <div className="flex gap-3">
            <div className="h-10 w-10 shrink-0 overflow-hidden rounded-full bg-slate-200">
              {user?.avatar_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={user.avatar_url} alt={user.username} className="h-full w-full object-cover" />
              ) : null}
            </div>
            <div className="flex-1 space-y-2">
              <textarea
                className="w-full rounded-xl border border-sky-100 bg-sky-50/70 p-3 text-sm outline-none ring-sky-300 transition focus:ring-2"
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
                  className="rounded-lg bg-sky-500 px-6 py-2 text-sm font-bold text-white transition hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {pendingComment ? "发布中..." : "发布评论"}
                </button>
              </div>
            </div>
          </div>

          <div className="space-y-6">
            {comments.map((item) => (
              <div key={item.id} className="space-y-3">
                <div className="flex gap-3">
                  <div className="h-10 w-10 overflow-hidden rounded-full bg-slate-200">
                    {item.user.avatar_url ? (
                      // eslint-disable-next-line @next/next/no-img-element
                      <img src={item.user.avatar_url} alt={commentAuthorName(item)} className="h-full w-full object-cover" />
                    ) : null}
                  </div>
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-bold text-sky-600">{commentAuthorName(item)}</span>
                      <span className="text-xs text-slate-400">{formatDate(item.created_at)}</span>
                    </div>
                    <p className="text-sm text-slate-800">{item.content}</p>
                    <div className="flex items-center gap-4 pt-1 text-xs text-slate-400">
                      <button className="flex items-center gap-1 transition-colors hover:text-sky-600" type="button">
                        <span className="material-symbols-outlined text-sm">thumb_up</span>
                        {formatCount(item.like_count)}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setReplyTargetId(replyTargetId === item.id ? null : item.id);
                          setReplyInput("");
                        }}
                        className="transition-colors hover:text-sky-600"
                      >
                        回复
                      </button>
                    </div>
                  </div>
                </div>

                {replyTargetId === item.id ? (
                  <div className="ml-[52px] rounded-xl border border-sky-100 bg-sky-50/60 p-3">
                    <textarea
                      rows={2}
                      value={replyInput}
                      onChange={(event) => setReplyInput(event.target.value)}
                      placeholder={`回复 @${commentAuthorName(item)}`}
                      className="w-full rounded-lg border border-sky-100 bg-white p-2 text-sm outline-none ring-sky-300 focus:ring-2"
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
                        className="rounded-lg bg-sky-500 px-3 py-1 text-xs font-semibold text-white"
                        onClick={() => publishComment({ content: replyInput, parent_comment_id: item.id })}
                        disabled={pendingComment}
                      >
                        回复
                      </button>
                    </div>
                  </div>
                ) : null}

                {item.replies.length > 0 ? (
                  <div className="ml-[52px] space-y-3 rounded-xl border border-slate-100 bg-white/90 p-3">
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

            {comments.length === 0 ? (
              <EmptyState title="还没有评论" description="成为第一个评论的人吧。" />
            ) : null}
          </div>
        </section>
      </div>

      <aside className="space-y-4">
        <div className="mb-1 flex items-center justify-between">
          <h3 className="text-lg font-bold text-slate-900">推荐视频</h3>
          <button type="button" onClick={() => void fetchPageData()} className="flex items-center gap-1 text-xs font-medium text-sky-600">
            <span className="material-symbols-outlined text-sm">autorenew</span>
            换一换
          </button>
        </div>

        <div className="space-y-4">
          {recommendationCards.map((video) => (
            <Link key={video.id} href={`/videos/${video.id}`} className="group flex gap-3">
              <div className="relative h-24 w-40 shrink-0 overflow-hidden rounded-lg">
                {video.cover_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img
                    src={video.cover_url}
                    alt={video.title}
                    className="h-full w-full object-cover transition-transform group-hover:scale-105"
                  />
                ) : (
                  <div className="h-full w-full bg-gradient-to-br from-slate-100 to-slate-200" />
                )}
              </div>
              <div className="flex flex-col justify-between py-0.5">
                <h4 className="line-clamp-2 text-sm font-bold leading-snug text-slate-900 transition-colors group-hover:text-sky-500">
                  {video.title}
                </h4>
                <div className="space-y-0.5">
                  <p className="text-xs text-slate-500">{video.author.username}</p>
                  <p className="flex items-center gap-1 text-[10px] text-slate-400">
                    <span className="material-symbols-outlined text-[12px]">play_circle</span>
                    {formatCount(video.views_count)} 播放
                  </p>
                </div>
              </div>
            </Link>
          ))}

          {recommendationCards.length === 0 ? (
            <EmptyState title="暂无推荐视频" description="稍后再试试看。" />
          ) : null}
        </div>
      </aside>
    </div>
  );
}
