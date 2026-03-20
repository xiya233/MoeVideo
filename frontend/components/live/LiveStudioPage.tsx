"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon } from "@/components/common/AppIcon";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadingSkeleton } from "@/components/common/LoadingSkeleton";
import type { Category, LiveSession } from "@/lib/dto";
import { mapCategory, mapCreateLiveSessionData, mapLiveSessionData } from "@/lib/dto/mappers";
import { liveApi } from "@/lib/live/api";
import { cn } from "@/lib/utils/cn";

type LiveVisibility = "public" | "private" | "unlisted";

function visibilityLabel(value: LiveVisibility): string {
  if (value === "private") {
    return "私密";
  }
  if (value === "unlisted") {
    return "非公开";
  }
  return "公开";
}

function statusLabel(status: LiveSession["status"]): string {
  if (status === "live") {
    return "直播中";
  }
  if (status === "ended") {
    return "已结束";
  }
  if (status === "failed") {
    return "已失败";
  }
  return "待开播";
}

function statusClass(status: LiveSession["status"]): string {
  if (status === "live") {
    return "bg-rose-100 text-rose-700";
  }
  if (status === "ended") {
    return "bg-emerald-100 text-emerald-700";
  }
  if (status === "failed") {
    return "bg-amber-100 text-amber-700";
  }
  return "bg-sky-100 text-sky-700";
}

function normalizeTagsInput(input: string): string[] {
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

async function copyText(text: string): Promise<boolean> {
  const value = text.trim();
  if (!value) {
    return false;
  }
  if (typeof navigator !== "undefined" && navigator.clipboard && navigator.clipboard.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      return false;
    }
  }
  return false;
}

export function LiveStudioPage() {
  const { ready, session, request, openAuthDialog } = useAuth();

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [categories, setCategories] = useState<Category[]>([]);
  const [currentSession, setCurrentSession] = useState<LiveSession | null>(null);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [categoryID, setCategoryID] = useState<number | null>(null);
  const [tagsInput, setTagsInput] = useState("");
  const [visibility, setVisibility] = useState<LiveVisibility>("public");

  const [createPending, setCreatePending] = useState(false);
  const [endPending, setEndPending] = useState(false);
  const [actionMessage, setActionMessage] = useState("");
  const [actionError, setActionError] = useState("");

  const streamURL = useMemo(() => {
    if (!currentSession) {
      return "";
    }
    return `${currentSession.publish_url.replace(/\/+$/, "")}/${currentSession.stream_key}`;
  }, [currentSession]);

  const loadPageData = useCallback(async () => {
    if (!ready) {
      return;
    }
    if (!session) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const [categoriesRaw, currentRaw] = await Promise.all([
        request<Category[]>("/categories", { auth: false }),
        liveApi.getCurrentSession(request),
      ]);
      const mappedCategories = (categoriesRaw ?? []).map(mapCategory);
      setCategories(mappedCategories);
      if (mappedCategories.length > 0) {
        setCategoryID((prev) => prev ?? mappedCategories[0].id);
      }
      const mappedCurrent = mapLiveSessionData(currentRaw);
      setCurrentSession(mappedCurrent.session);
    } catch (err) {
      setError(err instanceof Error ? err.message : "开播中心加载失败");
    } finally {
      setLoading(false);
    }
  }, [ready, request, session]);

  useEffect(() => {
    void loadPageData();
  }, [loadPageData]);

  useEffect(() => {
    if (!ready || !session) {
      return;
    }
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const raw = await liveApi.getCurrentSession(request);
          const mapped = mapLiveSessionData(raw);
          setCurrentSession(mapped.session);
        } catch {
          // Keep page stable; next manual refresh/action can recover state.
        }
      })();
    }, 5000);
    return () => window.clearInterval(timer);
  }, [ready, request, session]);

  const onCreate = async () => {
    if (!session) {
      openAuthDialog("login");
      return;
    }
    const normalizedTitle = title.trim();
    if (!normalizedTitle) {
      setActionError("请先填写直播标题");
      return;
    }
    if (!categoryID) {
      setActionError("请选择分类");
      return;
    }

    setCreatePending(true);
    setActionError("");
    setActionMessage("");
    try {
      const createdRaw = await liveApi.createSession(request, {
        title: normalizedTitle,
        description: description.trim(),
        category_id: categoryID,
        tags: normalizeTagsInput(tagsInput),
        visibility,
      });
      const created = mapCreateLiveSessionData(createdRaw);
      setCurrentSession(created.session);
      setActionMessage("直播会话已创建，请在 OBS 中使用推流地址和串流密钥开始推流。");
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "创建直播会话失败");
    } finally {
      setCreatePending(false);
    }
  };

  const onEnd = async () => {
    if (!currentSession) {
      return;
    }
    setEndPending(true);
    setActionError("");
    setActionMessage("");
    try {
      await liveApi.endCurrentSession(request);
      setActionMessage("直播已结束，系统将自动处理回放发布。");
      await loadPageData();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "结束直播失败");
    } finally {
      setEndPending(false);
    }
  };

  if (!ready) {
    return <LoadingSkeleton className="h-80 rounded-2xl" />;
  }

  if (!session) {
    return (
      <section className="space-y-4 rounded-2xl border border-primary/10 bg-white p-8 text-center shadow-sm">
        <EmptyState title="请先登录后开播" description="登录后即可创建直播会话，获取 OBS 推流配置。" />
        <button
          type="button"
          onClick={() => openAuthDialog("login")}
          className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-bold text-white shadow-sm shadow-primary/25 transition hover:opacity-90"
        >
          <AppIcon name="account_circle" size={16} />
          去登录
        </button>
      </section>
    );
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <LoadingSkeleton className="h-32 rounded-2xl" />
        <LoadingSkeleton className="h-[420px] rounded-2xl" />
      </div>
    );
  }

  if (error) {
    return <EmptyState title="开播中心加载失败" description={error} />;
  }

  return (
    <div className="space-y-6">
      <section className="rounded-2xl border border-primary/10 bg-white p-6 shadow-sm">
        <h1 className="flex items-center gap-2 text-2xl font-bold text-slate-900">
          <AppIcon name="radio" size={22} className="text-rose-600" />
          开播中心
        </h1>
        <p className="mt-2 text-sm text-slate-500">创建直播会话后，使用 OBS 推流。停播后系统会自动生成回放视频。</p>
      </section>

      {currentSession ? (
        <section className="space-y-4 rounded-2xl border border-primary/10 bg-white p-6 shadow-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className={cn("rounded-full px-3 py-1 text-xs font-bold", statusClass(currentSession.status))}>
              {statusLabel(currentSession.status)}
            </span>
            <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
              {visibilityLabel(currentSession.visibility)}
            </span>
            <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600">
              视频ID: {currentSession.video_id}
            </span>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <p className="text-xs font-bold text-slate-500">推流地址（OBS Server）</p>
              <div className="rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-700">
                {currentSession.publish_url}
              </div>
              <button
                type="button"
                onClick={() => void copyText(currentSession.publish_url)}
                className="rounded-lg border border-slate-200 px-3 py-1.5 text-xs font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
              >
                复制推流地址
              </button>
            </div>

            <div className="space-y-2">
              <p className="text-xs font-bold text-slate-500">串流密钥（OBS Stream Key）</p>
              <div className="rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-700">
                {currentSession.stream_key}
              </div>
              <button
                type="button"
                onClick={() => void copyText(currentSession.stream_key)}
                className="rounded-lg border border-slate-200 px-3 py-1.5 text-xs font-medium text-slate-700 transition-colors hover:border-primary/30 hover:text-primary"
              >
                复制串流密钥
              </button>
            </div>
          </div>

          <div className="space-y-2">
            <p className="text-xs font-bold text-slate-500">完整推流 URL（可直接用于排障）</p>
            <div className="rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-700 break-all">
              {streamURL}
            </div>
          </div>

          <div className="space-y-2">
            <p className="text-xs font-bold text-slate-500">直播播放地址</p>
            <a
              href={currentSession.playback_url}
              target="_blank"
              rel="noreferrer noopener"
              className="inline-flex items-center gap-1 rounded-lg border border-primary/20 bg-primary/5 px-3 py-2 text-xs font-medium text-primary hover:bg-primary/10"
            >
              <AppIcon name="play_circle" size={14} />
              打开播放地址
            </a>
          </div>

          {(currentSession.status === "waiting" || currentSession.status === "live") ? (
            <button
              type="button"
              onClick={() => void onEnd()}
              disabled={endPending}
              className="rounded-lg border border-rose-200 px-4 py-2 text-sm font-bold text-rose-600 transition-colors hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {endPending ? "结束中..." : "结束直播"}
            </button>
          ) : null}
        </section>
      ) : (
        <section className="space-y-5 rounded-2xl border border-primary/10 bg-white p-6 shadow-sm">
          <h2 className="text-lg font-bold text-slate-900">创建直播会话</h2>

          <div className="space-y-2">
            <label className="text-sm font-medium text-slate-700">标题</label>
            <input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              className="w-full rounded-xl border border-slate-200 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/20"
              placeholder="输入直播标题"
            />
          </div>

          <div className="space-y-2">
            <label className="text-sm font-medium text-slate-700">描述（可选）</label>
            <textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              className="min-h-[90px] w-full rounded-xl border border-slate-200 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/20"
              placeholder="输入直播简介"
            />
          </div>

          <div className="grid gap-4 md:grid-cols-3">
            <div className="space-y-2">
              <label className="text-sm font-medium text-slate-700">分类</label>
              <select
                value={categoryID ?? ""}
                onChange={(event) => setCategoryID(Number(event.target.value) || null)}
                className="w-full rounded-xl border border-slate-200 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/20"
              >
                <option value="">请选择分类</option>
                {categories.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium text-slate-700">可见性</label>
              <select
                value={visibility}
                onChange={(event) => setVisibility((event.target.value as LiveVisibility) || "public")}
                className="w-full rounded-xl border border-slate-200 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/20"
              >
                <option value="public">公开</option>
                <option value="unlisted">非公开</option>
                <option value="private">私密</option>
              </select>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium text-slate-700">标签（逗号分隔）</label>
              <input
                value={tagsInput}
                onChange={(event) => setTagsInput(event.target.value)}
                className="w-full rounded-xl border border-slate-200 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/20"
                placeholder="例如：直播,日常"
              />
            </div>
          </div>

          <button
            type="button"
            onClick={() => void onCreate()}
            disabled={createPending}
            className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-bold text-white shadow-sm shadow-primary/25 transition hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <AppIcon name="radio" size={16} />
            {createPending ? "创建中..." : "创建并获取推流配置"}
          </button>
        </section>
      )}

      {actionMessage ? (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{actionMessage}</div>
      ) : null}
      {actionError ? (
        <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{actionError}</div>
      ) : null}
    </div>
  );
}
