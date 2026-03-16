"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon } from "@/components/common/AppIcon";
import { EmptyState } from "@/components/common/EmptyState";
import type { Category, ImportJob, ImportJobDetailData, ImportItem, ImportJobsData, TorrentInspectResult } from "@/lib/dto";
import {
  mapCategory,
  mapImportJob,
  mapImportJobDetailData,
  mapImportJobsData,
  mapImportItem,
  mapTorrentInspectResult,
} from "@/lib/dto/mappers";
import { importsApi } from "@/lib/imports/api";
import { cn } from "@/lib/utils/cn";

const POLL_INTERVAL = 1000;
const MAX_PREVIEW_JOBS = 10;
type ImportSourceType = "url" | "torrent";
type TypedJobState = {
  items: ImportJob[];
  nextCursor: string;
  loading: boolean;
  loadingMore: boolean;
  loadMoreError: string;
  error: string;
  loaded: boolean;
};

type ActiveJobState = {
  id: string;
  detail: ImportJobDetailData | null;
  error: string;
};

function createEmptyJobState(): TypedJobState {
  return {
    items: [],
    nextCursor: "",
    loading: false,
    loadingMore: false,
    loadMoreError: "",
    error: "",
    loaded: false,
  };
}

function createEmptyActiveJobState(): ActiveJobState {
  return {
    id: "",
    detail: null,
    error: "",
  };
}

function readFileAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("读取种子文件失败"));
    reader.onload = () => {
      const raw = typeof reader.result === "string" ? reader.result : "";
      const idx = raw.indexOf(",");
      resolve(idx >= 0 ? raw.slice(idx + 1) : raw);
    };
    reader.readAsDataURL(file);
  });
}

function normalizeTags(input: string): string[] {
  return input
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, 10);
}

function formatBytes(value: number): string {
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  if (value < 1024 * 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  }
  return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatSpeed(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B/s";
  }
  return `${formatBytes(value)}/s`;
}

function isCancelledJob(status: ImportJob["status"], errorMessage?: string): boolean {
  if (status !== "failed") {
    return false;
  }
  const message = (errorMessage ?? "").toLowerCase();
  return message.includes("cancelled by user");
}

function formatStatus(status: ImportJob["status"], draftExpired = false, errorMessage?: string): string {
  if (status === "draft" && draftExpired) {
    return "已过期";
  }
  if (isCancelledJob(status, errorMessage)) {
    return "已取消";
  }
  switch (status) {
    case "draft":
      return "待开始";
    case "queued":
      return "排队中";
    case "downloading":
      return "下载中";
    case "succeeded":
      return "成功";
    case "partial":
      return "部分成功";
    case "failed":
      return "失败";
    default:
      return status;
  }
}

function isJobPollingStatus(status: ImportJob["status"]): boolean {
  return status === "queued" || status === "downloading";
}

function itemStatusLabel(status: ImportItem["status"]): string {
  switch (status) {
    case "pending":
      return "待处理";
    case "downloading":
      return "下载中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "skipped":
      return "已跳过";
    default:
      return status;
  }
}

function importJobTitle(job: ImportJob): string {
  return job.source_filename?.trim() || job.source_url?.trim() || "导入任务";
}

export function ImportPage() {
  const { ready, session, openAuthDialog, request } = useAuth();

  const [categories, setCategories] = useState<Category[]>([]);
  const [loadingCategories, setLoadingCategories] = useState(false);

  const [torrentFile, setTorrentFile] = useState<File | null>(null);
  const [importMode, setImportMode] = useState<"torrent" | "url">("torrent");
  const [urlInput, setURLInput] = useState("");
  const [inspectJob, setInspectJob] = useState<ImportJob | null>(null);
  const [inspectItems, setInspectItems] = useState<ImportItem[]>([]);
  const [selectedIndexes, setSelectedIndexes] = useState<Set<number>>(new Set());

  const [categoryID, setCategoryID] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private" | "unlisted">("public");
  const [tagInput, setTagInput] = useState("");
  const [importTitle, setImportTitle] = useState("");
  const [importTitlePrefix, setImportTitlePrefix] = useState("");
  const [importDescription, setImportDescription] = useState("");

  const [inspectPending, setInspectPending] = useState(false);
  const [startPending, setStartPending] = useState(false);
  const [urlPending, setURLPending] = useState(false);
  const [inspectError, setInspectError] = useState("");
  const [startError, setStartError] = useState("");
  const [urlError, setURLError] = useState("");

  const [activeJobsByType, setActiveJobsByType] = useState<Record<ImportSourceType, ActiveJobState>>({
    url: createEmptyActiveJobState(),
    torrent: createEmptyActiveJobState(),
  });

  const [jobsByType, setJobsByType] = useState<Record<ImportSourceType, TypedJobState>>({
    url: createEmptyJobState(),
    torrent: createEmptyJobState(),
  });
  const [jobsClearingScope, setJobsClearingScope] = useState<"finished" | "expired" | null>(null);
  const [cancelingJobID, setCancelingJobID] = useState("");

  const pollRef = useRef<number | null>(null);
  const pollMetaRef = useRef<{ sourceType: ImportSourceType; jobID: string } | null>(null);
  const fieldClass =
    "w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary disabled:cursor-not-allowed disabled:opacity-70";
  const fieldLabelClass = "text-xs font-semibold uppercase tracking-wide text-slate-500";
  const cardClass = "rounded-xl border border-primary/5 bg-white p-6 shadow-sm md:p-10";

  const selectedCount = selectedIndexes.size;

  const stopPolling = useCallback(() => {
    if (pollRef.current !== null) {
      window.clearInterval(pollRef.current);
      pollRef.current = null;
    }
    pollMetaRef.current = null;
  }, []);

  const loadCategories = useCallback(async () => {
    setLoadingCategories(true);
    try {
      const data = await request<Category[]>("/categories", { auth: false });
      setCategories((data ?? []).map(mapCategory));
    } catch {
      setCategories([]);
    } finally {
      setLoadingCategories(false);
    }
  }, [request]);

  const loadJobsBySourceType = useCallback(
    async (sourceType: ImportSourceType, cursor?: string) => {
      if (!session) {
        setJobsByType({
          url: createEmptyJobState(),
          torrent: createEmptyJobState(),
        });
        return;
      }

      const isLoadMore = Boolean(cursor);
      setJobsByType((prev) => {
        const current = prev[sourceType];
        return {
          ...prev,
          [sourceType]: {
            ...current,
            loading: isLoadMore ? current.loading : true,
            loadingMore: isLoadMore,
            loadMoreError: isLoadMore ? "" : current.loadMoreError,
            error: isLoadMore ? current.error : "",
          },
        };
      });

      try {
        const dataRaw = await importsApi.listJobs(request, {
          limit: MAX_PREVIEW_JOBS,
          cursor,
          source_type: sourceType,
        });
        const data = mapImportJobsData(dataRaw as ImportJobsData);

        setJobsByType((prev) => {
          const current = prev[sourceType];
          let nextItems: ImportJob[] = data.items;
          if (isLoadMore) {
            const seen = new Set(current.items.map((item) => item.id));
            nextItems = [...current.items];
            for (const item of data.items) {
              if (seen.has(item.id)) {
                continue;
              }
              seen.add(item.id);
              nextItems.push(item);
            }
          }

          return {
            ...prev,
            [sourceType]: {
              ...current,
              items: nextItems,
              nextCursor: data.next_cursor ?? "",
              loading: false,
              loadingMore: false,
              loadMoreError: "",
              error: "",
              loaded: true,
            },
          };
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : isLoadMore ? "加载更多失败" : "任务列表加载失败";
        setJobsByType((prev) => {
          const current = prev[sourceType];
          return {
            ...prev,
            [sourceType]: {
              ...current,
              loading: false,
              loadingMore: false,
              loadMoreError: isLoadMore ? message : "",
              error: isLoadMore ? current.error : message,
            },
          };
        });
      }
    },
    [request, session],
  );

  const loadMoreJobs = useCallback(async () => {
    const current = jobsByType[importMode];
    if (!session || current.loadingMore || !current.nextCursor) {
      return;
    }
    await loadJobsBySourceType(importMode, current.nextCursor);
  }, [jobsByType, importMode, loadJobsBySourceType, session]);

  const loadJobDetail = useCallback(
    async (sourceType: ImportSourceType, jobID: string) => {
      const dataRaw = await importsApi.getJob(request, jobID);
      const data = mapImportJobDetailData(dataRaw);
      setActiveJobsByType((prev) => ({
        ...prev,
        [sourceType]: {
          id: jobID,
          detail: data,
          error: "",
        },
      }));
      return data;
    },
    [request],
  );

  useEffect(() => {
    if (!session) {
      setJobsByType({
        url: createEmptyJobState(),
        torrent: createEmptyJobState(),
      });
      setActiveJobsByType({
        url: createEmptyActiveJobState(),
        torrent: createEmptyActiveJobState(),
      });
      stopPolling();
      return;
    }
    void loadCategories();
  }, [loadCategories, session, stopPolling]);

  const currentJobsState = jobsByType[importMode];
  const currentActiveJobState = activeJobsByType[importMode];

  useEffect(() => {
    if (!session) {
      return;
    }
    if (currentJobsState.loaded || currentJobsState.loading) {
      return;
    }
    void loadJobsBySourceType(importMode);
  }, [currentJobsState.loaded, currentJobsState.loading, importMode, loadJobsBySourceType, session]);

  useEffect(() => {
    return () => {
      stopPolling();
    };
  }, [stopPolling]);

  const openPolling = useCallback(
    (sourceType: ImportSourceType, jobID: string) => {
      stopPolling();
      pollMetaRef.current = { sourceType, jobID };
      pollRef.current = window.setInterval(() => {
        void (async () => {
          try {
            const next = await loadJobDetail(sourceType, jobID);
            if (!isJobPollingStatus(next.job.status)) {
              stopPolling();
              await loadJobsBySourceType(next.job.source_type);
            }
          } catch (err) {
            const message = err instanceof Error ? err.message : "导入任务刷新失败";
            setActiveJobsByType((prev) => ({
              ...prev,
              [sourceType]: {
                ...prev[sourceType],
                error: message,
              },
            }));
          }
        })();
      }, POLL_INTERVAL);
    },
    [loadJobDetail, loadJobsBySourceType, stopPolling],
  );

  useEffect(() => {
    if (!session) {
      return;
    }
    const runningPoll = pollMetaRef.current;
    if (runningPoll && runningPoll.sourceType !== importMode) {
      stopPolling();
    }
    const activeState = activeJobsByType[importMode];
    if (!activeState.id || !activeState.detail) {
      if (pollMetaRef.current?.sourceType === importMode) {
        stopPolling();
      }
      return;
    }
    if (!isJobPollingStatus(activeState.detail.job.status)) {
      if (pollMetaRef.current?.sourceType === importMode) {
        stopPolling();
      }
      return;
    }
    const currentPoll = pollMetaRef.current;
    if (!currentPoll || currentPoll.sourceType !== importMode || currentPoll.jobID !== activeState.id) {
      openPolling(importMode, activeState.id);
    }
  }, [activeJobsByType, importMode, openPolling, session, stopPolling]);

  const onInspectTorrent = async () => {
    if (!torrentFile) {
      setInspectError("请先选择 .torrent 文件");
      return;
    }
    if (!torrentFile.name.toLowerCase().endsWith(".torrent")) {
      setInspectError("只支持上传 .torrent 文件");
      return;
    }

    setInspectPending(true);
    setInspectError("");
    setStartError("");
    setURLError("");

    try {
      const base64 = await readFileAsBase64(torrentFile);
      const resultRaw = await importsApi.inspectTorrent(request, {
        filename: torrentFile.name,
        torrent_base64: base64,
      });
      const result = mapTorrentInspectResult(resultRaw as TorrentInspectResult);
      setInspectJob(mapImportJob(result.job));
      const mappedItems = (result.items ?? []).map(mapImportItem);
      setInspectItems(mappedItems);
      const persistedSelected = mappedItems.filter((item) => item.selected).map((item) => item.file_index);
      setSelectedIndexes(new Set(persistedSelected.length > 0 ? persistedSelected : mappedItems.map((item) => item.file_index)));
      setActiveJobsByType((prev) => ({
        ...prev,
        torrent: {
          ...prev.torrent,
          id: result.job.id,
          error: "",
        },
      }));
      const detail = await loadJobDetail("torrent", result.job.id);
      if (isJobPollingStatus(detail.job.status)) {
        openPolling("torrent", result.job.id);
      } else {
        stopPolling();
      }
      await loadJobsBySourceType("torrent");
    } catch (err) {
      setInspectJob(null);
      setInspectItems([]);
      setSelectedIndexes(new Set());
      setInspectError(err instanceof Error ? err.message : "解析种子失败");
    } finally {
      setInspectPending(false);
    }
  };

  const onStartImport = async () => {
    if (!inspectJob) {
      setStartError("请先完成种子解析");
      return;
    }
    if (inspectJob.draft_expired) {
      setStartError("草稿已过期，请重新解析种子");
      return;
    }
    if (selectedIndexes.size === 0) {
      setStartError("至少勾选一个视频文件");
      return;
    }
    if (!categoryID) {
      setStartError("请选择分类");
      return;
    }

    setStartPending(true);
    setStartError("");
    setURLError("");

    try {
      await importsApi.startTorrent(request, {
        job_id: inspectJob.id,
        selected_file_indexes: Array.from(selectedIndexes).sort((a, b) => a - b),
        category_id: Number(categoryID),
        tags: normalizeTags(tagInput),
        visibility,
        title: importTitle.trim() || undefined,
        title_prefix: importTitlePrefix.trim() || undefined,
        description: importDescription.trim() || undefined,
      });

      setActiveJobsByType((prev) => ({
        ...prev,
        torrent: {
          ...prev.torrent,
          id: inspectJob.id,
          error: "",
        },
      }));
      const detail = await loadJobDetail("torrent", inspectJob.id);
      if (isJobPollingStatus(detail.job.status)) {
        openPolling("torrent", inspectJob.id);
      }
      await loadJobsBySourceType("torrent");
    } catch (err) {
      setStartError(err instanceof Error ? err.message : "开始导入失败");
    } finally {
      setStartPending(false);
    }
  };

  const onStartURLImport = async () => {
    const trimmedURL = urlInput.trim();
    if (!trimmedURL) {
      setURLError("请输入视频页面 URL");
      return;
    }
    if (!categoryID) {
      setURLError("请选择分类");
      return;
    }

    setURLPending(true);
    setURLError("");
    setInspectError("");
    setStartError("");

    try {
      const result = await importsApi.startURL(request, {
        url: trimmedURL,
        category_id: Number(categoryID),
        tags: normalizeTags(tagInput),
        visibility,
        title: importTitle.trim() || undefined,
        description: importDescription.trim() || undefined,
      });
      setActiveJobsByType((prev) => ({
        ...prev,
        url: {
          ...prev.url,
          id: result.job_id,
          error: "",
        },
      }));
      const detail = await loadJobDetail("url", result.job_id);
      if (isJobPollingStatus(detail.job.status)) {
        openPolling("url", result.job_id);
      } else {
        stopPolling();
      }
      await loadJobsBySourceType("url");
    } catch (err) {
      setURLError(err instanceof Error ? err.message : "URL 导入失败");
    } finally {
      setURLPending(false);
    }
  };

  const toggleIndex = (fileIndex: number) => {
    setSelectedIndexes((prev) => {
      const next = new Set(prev);
      if (next.has(fileIndex)) {
        next.delete(fileIndex);
      } else {
        next.add(fileIndex);
      }
      return next;
    });
  };

  const activeDetail = currentActiveJobState.detail;
  const activeJobError = currentActiveJobState.error;

  const applyDraftToEditor = (detail: ImportJobDetailData) => {
    const draftItems = detail.items;
    const persistedSelected = draftItems.filter((item) => item.selected).map((item) => item.file_index);
    const selected =
      persistedSelected.length > 0 ? persistedSelected : draftItems.map((item) => item.file_index);

    setImportMode("torrent");
    setInspectJob(detail.job);
    setInspectItems(draftItems);
    setSelectedIndexes(new Set(selected));
    setCategoryID(detail.job.category_id ? String(detail.job.category_id) : "");
    setTagInput(detail.job.tags.join(","));
    setVisibility(detail.job.visibility);
    setImportTitle(detail.job.custom_title ?? "");
    setImportTitlePrefix(detail.job.custom_title_prefix ?? "");
    setImportDescription(detail.job.custom_description ?? "");
    setInspectError("");
    setStartError("");
    setURLError("");
  };

  const resumeDraftJob = async (jobID: string) => {
    try {
      setActiveJobsByType((prev) => ({
        ...prev,
        torrent: {
          ...prev.torrent,
          id: jobID,
          error: "",
        },
      }));
      const detail = await loadJobDetail("torrent", jobID);
      if (detail.job.source_type !== "torrent" || detail.job.status !== "draft") {
        setStartError("当前任务不是可继续的 BT 草稿");
        return;
      }
      if (detail.job.draft_expired) {
        setStartError("草稿已过期，请重新解析种子");
        return;
      }
      applyDraftToEditor(detail);
      stopPolling();
    } catch (err) {
      const message = err instanceof Error ? err.message : "恢复草稿失败";
      setJobsByType((prev) => ({
        ...prev,
        [importMode]: {
          ...prev[importMode],
          error: message,
        },
      }));
    }
  };

  const clearJobsByScope = async (scope: "finished" | "expired") => {
    if (!session || jobsClearingScope) {
      return;
    }
    if (scope === "expired" && importMode !== "torrent") {
      return;
    }
    const confirmText =
      scope === "expired"
        ? "确认清理当前 BT 类型的已过期草稿吗？此操作不可撤销。"
        : `确认清理当前${importMode === "url" ? " URL " : " BT "}类型的已结束导入记录吗？此操作不可撤销。`;
    if (!window.confirm(confirmText)) {
      return;
    }
    setJobsClearingScope(scope);
    setJobsByType((prev) => ({
      ...prev,
      [importMode]: { ...prev[importMode], error: "", loadMoreError: "" },
    }));
    try {
      await importsApi.clearFinishedJobs(request, scope, importMode);
      const currentActiveDetail = activeJobsByType[importMode].detail;
      if (
        currentActiveDetail &&
        currentActiveDetail.job.source_type === importMode &&
        ((scope === "finished" && !isJobPollingStatus(currentActiveDetail.job.status) && currentActiveDetail.job.status !== "draft") ||
          (scope === "expired" &&
            currentActiveDetail.job.source_type === "torrent" &&
            currentActiveDetail.job.status === "draft" &&
            currentActiveDetail.job.draft_expired))
      ) {
        stopPolling();
        setActiveJobsByType((prev) => ({
          ...prev,
          [importMode]: createEmptyActiveJobState(),
        }));
      }
      if (scope === "expired" && importMode === "torrent" && inspectJob?.id) {
        try {
          const draftDetail = await loadJobDetail("torrent", inspectJob.id);
          if (draftDetail.job.draft_expired) {
            setInspectJob(null);
            setInspectItems([]);
            setSelectedIndexes(new Set());
            setStartError("当前草稿已过期，请重新解析种子");
          }
        } catch {
          setInspectJob(null);
          setInspectItems([]);
          setSelectedIndexes(new Set());
        }
      }
      setJobsByType((prev) => ({
        ...prev,
        [importMode]: createEmptyJobState(),
      }));
      await loadJobsBySourceType(importMode);
    } catch (err) {
      const message = err instanceof Error ? err.message : "清理导入记录失败";
      setJobsByType((prev) => ({
        ...prev,
        [importMode]: {
          ...prev[importMode],
          error: message,
        },
      }));
    } finally {
      setJobsClearingScope(null);
    }
  };

  const cancelImportJob = useCallback(
    async (sourceType: ImportSourceType, jobID: string) => {
      if (!session || !jobID || cancelingJobID) {
        return;
      }
      if (!window.confirm("确认取消该导入任务吗？该任务将停止并标记为已取消。")) {
        return;
      }
      setCancelingJobID(jobID);
      setActiveJobsByType((prev) => ({
        ...prev,
        [sourceType]: {
          ...prev[sourceType],
          error: "",
        },
      }));
      try {
        await importsApi.cancelJob(request, jobID);
        if (pollMetaRef.current?.jobID === jobID) {
          stopPolling();
        }
        await loadJobsBySourceType(sourceType);
        const active = activeJobsByType[sourceType];
        if (active.id === jobID) {
          await loadJobDetail(sourceType, jobID);
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : "取消任务失败";
        setActiveJobsByType((prev) => ({
          ...prev,
          [sourceType]: {
            ...prev[sourceType],
            error: message,
          },
        }));
      } finally {
        setCancelingJobID("");
      }
    },
    [activeJobsByType, cancelingJobID, loadJobDetail, loadJobsBySourceType, request, session, stopPolling],
  );

  if (!ready) {
    return (
      <div className="mx-auto w-full max-w-5xl rounded-xl border border-primary/10 bg-white p-10 shadow-sm">
        <div className="space-y-4">
          <div className="h-8 w-52 animate-pulse rounded-lg bg-slate-100" />
          <div className="h-5 w-80 animate-pulse rounded-lg bg-slate-100" />
          <div className="h-11 w-32 animate-pulse rounded-full bg-slate-100" />
        </div>
      </div>
    );
  }

  if (!session) {
    return (
      <div className="mx-auto w-full max-w-5xl rounded-xl border border-primary/10 bg-white p-10 shadow-sm">
        <EmptyState title="请先登录后再导入" description="导入视频需要登录态，登录后可提交 URL 或 BT 种子任务。" />
        <div className="mt-6 flex justify-center">
          <button
            type="button"
            onClick={() => openAuthDialog("login")}
            className="rounded-full bg-primary px-6 py-2.5 text-sm font-bold text-white transition-all hover:bg-primary/90"
          >
            立即登录
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <section className={cardClass}>
        <div className="mb-4 flex items-center gap-2">
          <AppIcon name="input" className="text-primary" />
          <h1 className="text-xl font-bold text-slate-900">导入视频</h1>
        </div>
        <p className="text-sm text-slate-500">支持 URL 或 BT 种子导入，系统会自动下载后进入现有转码发布流程。</p>

        <div className="mt-5 grid grid-cols-1 gap-3 sm:grid-cols-2">
          <button
            type="button"
            onClick={() => setImportMode("url")}
            className={cn(
              "flex h-14 items-center justify-center gap-2 rounded-xl border px-5 text-sm font-semibold transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
              importMode === "url"
                ? "border-primary bg-primary/10 text-primary shadow-sm"
                : "border-slate-200 bg-slate-50 text-slate-700 hover:border-primary/40 hover:bg-primary/5 hover:text-slate-900",
            )}
          >
            <AppIcon name="input" size={18} className="shrink-0" />
            URL 导入
          </button>
          <button
            type="button"
            onClick={() => setImportMode("torrent")}
            className={cn(
              "flex h-14 items-center justify-center gap-2 rounded-xl border px-5 text-sm font-semibold transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
              importMode === "torrent"
                ? "border-primary bg-primary/10 text-primary shadow-sm"
                : "border-slate-200 bg-slate-50 text-slate-700 hover:border-primary/40 hover:bg-primary/5 hover:text-slate-900",
            )}
          >
            <AppIcon name="magnet" size={18} className="shrink-0" />
            BT 种子导入
          </button>
        </div>

        {importMode === "torrent" ? (
          <>
            <div className="mt-6 grid grid-cols-1 items-start gap-4 md:grid-cols-[minmax(0,1fr)_auto]">
              <label className="flex min-w-0 cursor-pointer items-center gap-3 rounded-xl border border-dashed border-primary/30 bg-primary/5 px-4 py-3 text-sm text-slate-700 transition hover:bg-primary/10">
                <AppIcon name="upload" size={18} className="text-primary" />
                <span className="min-w-0 flex-1 truncate" title={torrentFile ? torrentFile.name : "选择 .torrent 文件"}>
                  {torrentFile ? torrentFile.name : "选择 .torrent 文件"}
                </span>
                <input
                  type="file"
                  accept=".torrent,application/x-bittorrent"
                  className="hidden"
                  onChange={(event) => {
                    const file = event.target.files?.[0] ?? null;
                    setTorrentFile(file);
                    setInspectError("");
                  }}
                />
              </label>

              <button
                type="button"
                onClick={() => void onInspectTorrent()}
                disabled={inspectPending || !torrentFile}
                className="self-start rounded-xl bg-primary px-5 py-3 text-sm font-bold text-white transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {inspectPending ? "解析中..." : "解析种子"}
              </button>
            </div>

            {inspectError ? <p className="mt-3 text-sm text-rose-600">{inspectError}</p> : null}
          </>
        ) : (
          <>
            <div className="mt-6 space-y-4">
              <label className="space-y-1">
                <span className={fieldLabelClass}>视频 URL</span>
                <input
                  value={urlInput}
                  onChange={(event) => setURLInput(event.target.value)}
                  placeholder="https://example.com/video/123"
                  className={fieldClass}
                />
              </label>

              <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2">
                <label className="space-y-1">
                  <span className={fieldLabelClass}>标题（可选）</span>
                  <input
                    value={importTitle}
                    onChange={(event) => setImportTitle(event.target.value)}
                    placeholder="留空则自动生成标题"
                    className={fieldClass}
                  />
                </label>
                <label className="space-y-1">
                  <span className={fieldLabelClass}>描述（可选）</span>
                  <textarea
                    value={importDescription}
                    onChange={(event) => setImportDescription(event.target.value)}
                    rows={3}
                    placeholder="导入后写入视频描述"
                    className={`${fieldClass} resize-none`}
                  />
                </label>
              </div>

              <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
                <label className="space-y-1">
                  <span className={fieldLabelClass}>分类</span>
                  <select
                    className={`${fieldClass} appearance-none`}
                    value={categoryID}
                    onChange={(event) => setCategoryID(event.target.value)}
                    disabled={loadingCategories}
                  >
                    <option value="">请选择分类</option>
                    {categories.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="space-y-1">
                  <span className={fieldLabelClass}>可见性</span>
                  <select
                    className={`${fieldClass} appearance-none`}
                    value={visibility}
                    onChange={(event) => setVisibility(event.target.value as "public" | "private" | "unlisted")}
                  >
                    <option value="public">公开</option>
                    <option value="unlisted">非公开列表</option>
                    <option value="private">仅自己可见</option>
                  </select>
                </label>

                <label className="space-y-1">
                  <span className={fieldLabelClass}>标签（逗号分隔）</span>
                  <input
                    value={tagInput}
                    onChange={(event) => setTagInput(event.target.value)}
                    placeholder="例如：动漫,二创"
                    className={fieldClass}
                  />
                </label>
              </div>

              {urlError ? <p className="text-sm text-rose-600">{urlError}</p> : null}
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => void onStartURLImport()}
                  disabled={urlPending}
                  className="rounded-xl bg-primary px-5 py-3 text-sm font-bold text-white transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {urlPending ? "提交中..." : "开始导入"}
                </button>
              </div>
            </div>
          </>
        )}
      </section>

      {importMode === "torrent" ? (
        <section className={cardClass}>
        <div className="mb-4 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-bold text-slate-900">Step 2. 勾选导入与发布设置</h2>
            <p className="text-sm text-slate-500">已选 {selectedCount} 个文件</p>
          </div>
          {inspectJob ? <span className="text-xs text-slate-500">Job ID: {inspectJob.id.slice(0, 8)}...</span> : null}
        </div>

        {!inspectJob ? (
          <EmptyState title="暂无可导入任务" description="请先在上一步上传并解析种子。" />
        ) : (
          <>
            <div className="overflow-hidden rounded-xl border border-slate-200">
              <table className="min-w-full divide-y divide-slate-200 text-sm">
                <thead className="bg-slate-50">
                  <tr>
                    <th className="px-3 py-2 text-left font-semibold text-slate-600">导入</th>
                    <th className="px-3 py-2 text-left font-semibold text-slate-600">文件</th>
                    <th className="px-3 py-2 text-left font-semibold text-slate-600">大小</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 bg-white">
                  {inspectItems.length === 0 ? (
                    <tr>
                      <td colSpan={3} className="px-3 py-6 text-center text-slate-500">
                        种子内未发现可导入的视频文件
                      </td>
                    </tr>
                  ) : (
                    inspectItems.map((item) => {
                      const checked = selectedIndexes.has(item.file_index);
                      return (
                        <tr key={item.id}>
                          <td className="px-3 py-2">
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={() => toggleIndex(item.file_index)}
                              className="h-4 w-4 rounded border-slate-300 text-primary focus:ring-primary"
                            />
                          </td>
                          <td className="px-3 py-2 text-slate-800">{item.file_path}</td>
                          <td className="px-3 py-2 text-slate-500">{formatBytes(item.file_size_bytes)}</td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </div>

            <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-3">
              <label className="space-y-1 md:col-span-1">
                <span className={fieldLabelClass}>单文件标题（可选）</span>
                <input
                  value={importTitle}
                  onChange={(event) => setImportTitle(event.target.value)}
                  placeholder="仅勾选 1 个文件时生效"
                  className={fieldClass}
                />
              </label>

              <label className="space-y-1 md:col-span-1">
                <span className={fieldLabelClass}>多文件标题前缀（可选）</span>
                <input
                  value={importTitlePrefix}
                  onChange={(event) => setImportTitlePrefix(event.target.value)}
                  placeholder="勾选多个时：前缀 - 文件名"
                  className={fieldClass}
                />
              </label>

              <label className="space-y-1 md:col-span-1">
                <span className={fieldLabelClass}>描述（可选）</span>
                <input
                  value={importDescription}
                  onChange={(event) => setImportDescription(event.target.value)}
                  placeholder="统一应用到所有勾选视频"
                  className={fieldClass}
                />
              </label>
            </div>

            <p className="mt-2 text-xs text-slate-500">
              {selectedCount <= 1
                ? "当前为单文件模式：若填写“单文件标题”会覆盖自动标题。"
                : "当前为多文件模式：若填写“多文件标题前缀”，标题将按“前缀 - 文件名”生成。"}
            </p>

            <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-3">
              <label className="space-y-1">
                <span className={fieldLabelClass}>分类</span>
                <select
                  className={`${fieldClass} appearance-none`}
                  value={categoryID}
                  onChange={(event) => setCategoryID(event.target.value)}
                  disabled={loadingCategories}
                >
                  <option value="">请选择分类</option>
                  {categories.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>

              <label className="space-y-1">
                <span className={fieldLabelClass}>可见性</span>
                <select
                  className={`${fieldClass} appearance-none`}
                  value={visibility}
                  onChange={(event) => setVisibility(event.target.value as "public" | "private" | "unlisted")}
                >
                  <option value="public">公开</option>
                  <option value="unlisted">非公开列表</option>
                  <option value="private">仅自己可见</option>
                </select>
              </label>

              <label className="space-y-1 md:col-span-1">
                <span className={fieldLabelClass}>标签（逗号分隔）</span>
                <input
                  value={tagInput}
                  onChange={(event) => setTagInput(event.target.value)}
                  placeholder="例如：动漫,二创"
                  className={fieldClass}
                />
              </label>
            </div>

            {startError ? <p className="mt-3 text-sm text-rose-600">{startError}</p> : null}

            <div className="mt-5 flex items-center justify-end gap-3">
              <button
                type="button"
                onClick={() => {
                  setSelectedIndexes(new Set(inspectItems.map((item) => item.file_index)));
                }}
                className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition hover:border-primary/40 hover:text-primary"
              >
                全选
              </button>
              <button
                type="button"
                onClick={() => {
                  setSelectedIndexes(new Set());
                }}
                className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition hover:border-primary/40 hover:text-primary"
              >
                清空
              </button>
              <button
                type="button"
                onClick={() => void onStartImport()}
                disabled={startPending || inspectItems.length === 0 || Boolean(inspectJob?.draft_expired)}
                className="rounded-xl bg-primary px-5 py-2 text-sm font-bold text-white transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {startPending ? "提交中..." : "开始导入"}
              </button>
            </div>
          </>
        )}
        </section>
      ) : null}

      <section className={cardClass}>
        <div className="mb-4 flex items-center justify-between gap-3">
          <h2 className="text-lg font-bold text-slate-900">{importMode === "url" ? "URL 导入任务进度" : "BT 导入任务进度"}</h2>
          {activeDetail ? (
            <div className="flex items-center gap-3">
              <span
                className={cn(
                  "rounded-full px-3 py-1 text-xs font-semibold",
                  activeDetail.job.status === "succeeded"
                    ? "bg-emerald-100 text-emerald-700"
                    : activeDetail.job.status === "partial"
                      ? "bg-amber-100 text-amber-700"
                      : activeDetail.job.status === "draft" && activeDetail.job.draft_expired
                        ? "bg-slate-200 text-slate-700"
                      : isCancelledJob(activeDetail.job.status, activeDetail.job.error_message)
                        ? "bg-slate-200 text-slate-700"
                      : activeDetail.job.status === "failed"
                        ? "bg-rose-100 text-rose-700"
                        : "bg-sky-100 text-sky-700",
                )}
              >
                {formatStatus(activeDetail.job.status, activeDetail.job.draft_expired, activeDetail.job.error_message)}
              </span>
              {isJobPollingStatus(activeDetail.job.status) ? (
                <button
                  type="button"
                  onClick={() => void cancelImportJob(activeDetail.job.source_type, activeDetail.job.id)}
                  disabled={cancelingJobID === activeDetail.job.id}
                  className="rounded-lg border border-rose-200 px-3 py-1.5 text-xs font-semibold text-rose-700 transition hover:border-rose-300 hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {cancelingJobID === activeDetail.job.id ? "取消中..." : "取消任务"}
                </button>
              ) : null}
            </div>
          ) : null}
        </div>

        {activeJobError ? <p className="mb-3 text-sm text-rose-600">{activeJobError}</p> : null}

        {!activeDetail ? (
          <EmptyState title="暂无活跃任务" description="解析并开始导入后，这里会显示实时进度。" />
        ) : (
          <>
            <div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
              <p className="text-sm font-medium text-slate-700">{importJobTitle(activeDetail.job)}</p>
              {activeDetail.job.source_type === "torrent" && activeDetail.job.info_hash ? (
                <p className="mt-1 text-xs text-slate-500">InfoHash: {activeDetail.job.info_hash}</p>
              ) : activeDetail.job.source_url ? (
                <p className="mt-1 truncate text-xs text-slate-500">URL: {activeDetail.job.source_url}</p>
              ) : null}
              <div className="mt-3 h-2 overflow-hidden rounded-full bg-slate-200">
                <div className="h-full rounded-full bg-primary" style={{ width: `${Math.min(100, Math.max(0, activeDetail.job.progress))}%` }} />
              </div>
              <p className="mt-2 text-xs text-slate-500">
                已完成 {activeDetail.job.completed_files}/{activeDetail.job.selected_files} · 失败 {activeDetail.job.failed_files}
              </p>
              {activeDetail.job.source_type === "url" ? (
                <div className="mt-3 grid grid-cols-2 gap-3 text-xs text-slate-600">
                  <div className="rounded-lg bg-white px-3 py-2">
                    <span className="text-slate-500">下载速度</span>
                    <p className="mt-1 font-semibold text-slate-900">{formatSpeed(activeDetail.job.download_speed_bps)}</p>
                  </div>
                  <div className="rounded-lg bg-white px-3 py-2">
                    <span className="text-slate-500">已下载</span>
                    <p className="mt-1 font-semibold text-slate-900">{formatBytes(activeDetail.job.downloaded_bytes)}</p>
                  </div>
                </div>
              ) : (
                <div className="mt-3 grid grid-cols-2 gap-3 text-xs text-slate-600 md:grid-cols-4">
                  <div className="rounded-lg bg-white px-3 py-2">
                    <span className="text-slate-500">下载速度</span>
                    <p className="mt-1 font-semibold text-slate-900">{formatSpeed(activeDetail.job.download_speed_bps)}</p>
                  </div>
                  <div className="rounded-lg bg-white px-3 py-2">
                    <span className="text-slate-500">上传速度</span>
                    <p className="mt-1 font-semibold text-slate-900">{formatSpeed(activeDetail.job.upload_speed_bps)}</p>
                  </div>
                  <div className="rounded-lg bg-white px-3 py-2">
                    <span className="text-slate-500">已下载</span>
                    <p className="mt-1 font-semibold text-slate-900">{formatBytes(activeDetail.job.downloaded_bytes)}</p>
                  </div>
                  <div className="rounded-lg bg-white px-3 py-2">
                    <span className="text-slate-500">已上传</span>
                    <p className="mt-1 font-semibold text-slate-900">{formatBytes(activeDetail.job.uploaded_bytes)}</p>
                  </div>
                </div>
              )}
              {activeDetail.job.error_message && !isCancelledJob(activeDetail.job.status, activeDetail.job.error_message) ? (
                <p className="mt-2 text-xs text-rose-600">{activeDetail.job.error_message}</p>
              ) : null}
            </div>

            <div className="mt-4 overflow-hidden rounded-xl border border-slate-200">
              <table className="min-w-full divide-y divide-slate-200 text-sm">
                <thead className="bg-slate-50">
                  <tr>
                    <th className="px-3 py-2 text-left font-semibold text-slate-600">状态</th>
                    <th className="px-3 py-2 text-left font-semibold text-slate-600">文件</th>
                    <th className="px-3 py-2 text-left font-semibold text-slate-600">视频</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 bg-white">
                  {activeDetail.items.map((item) => (
                    <tr key={item.id}>
                      <td className="px-3 py-2 text-slate-700">{itemStatusLabel(item.status)}</td>
                      <td className="px-3 py-2 text-slate-800">{item.file_path}</td>
                      <td className="px-3 py-2">
                        {item.video_id ? (
                          <Link href={`/videos/${item.video_id}`} className="text-primary hover:underline">
                            查看视频
                          </Link>
                        ) : (
                          <span className="text-slate-400">-</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </section>

      <section className={cardClass}>
        <div className="mb-4 flex items-center justify-between gap-3">
          <h2 className="text-lg font-bold text-slate-900">{importMode === "url" ? "最近 URL 导入任务" : "最近 BT 导入任务"}</h2>
          <div className="flex items-center gap-2">
            {importMode === "torrent" ? (
              <button
                type="button"
                onClick={() => void clearJobsByScope("expired")}
                disabled={Boolean(jobsClearingScope)}
                className="rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold text-slate-700 transition hover:border-primary/40 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
              >
                {jobsClearingScope === "expired" ? "清理中..." : "清理已过期BT任务"}
              </button>
            ) : null}
            <button
              type="button"
              onClick={() => void clearJobsByScope("finished")}
              disabled={Boolean(jobsClearingScope)}
              className="rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold text-slate-700 transition hover:border-primary/40 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
            >
              {jobsClearingScope === "finished" ? "清理中..." : importMode === "url" ? "清理已完成URL任务" : "清理已完成BT任务"}
            </button>
          </div>
        </div>
        {currentJobsState.error ? <p className="mb-3 text-sm text-rose-600">{currentJobsState.error}</p> : null}

        {currentJobsState.loading ? (
          <p className="text-sm text-slate-500">任务列表加载中...</p>
        ) : currentJobsState.items.length === 0 ? (
          <EmptyState title="暂无导入记录" description="导入任务提交后会显示在这里。" />
        ) : (
          <>
            <div className="space-y-3">
              {currentJobsState.items.map((job) => (
                <div
                  key={job.id}
                  className="rounded-xl border border-slate-100 bg-slate-50 px-4 py-3 transition hover:border-primary/40"
                >
                  <div className="flex items-center justify-between gap-3">
                    <button
                      type="button"
                      className="min-w-0 flex-1 text-left"
                      onClick={() => {
                        setActiveJobsByType((prev) => ({
                          ...prev,
                          [importMode]: {
                            ...prev[importMode],
                            id: job.id,
                            error: "",
                          },
                        }));
                        void loadJobDetail(importMode, job.id);
                        if (isJobPollingStatus(job.status)) {
                          openPolling(importMode, job.id);
                        } else {
                          stopPolling();
                        }
                      }}
                    >
                      <p className="truncate text-sm font-medium text-slate-800">{importJobTitle(job)}</p>
                      <p className="mt-1 text-xs text-slate-500">
                        {job.completed_files}/{job.selected_files} 完成 · 失败 {job.failed_files}
                      </p>
                    </button>
                    <div className="shrink-0 flex items-center gap-2">
                      {job.source_type === "torrent" && job.status === "draft" && !job.draft_expired ? (
                        <button
                          type="button"
                          onClick={() => void resumeDraftJob(job.id)}
                          className="rounded-lg border border-primary/30 px-3 py-1.5 text-xs font-semibold text-primary transition hover:border-primary hover:bg-primary/5"
                        >
                          继续导入
                        </button>
                      ) : null}
                      {isJobPollingStatus(job.status) ? (
                        <button
                          type="button"
                          onClick={() => void cancelImportJob(job.source_type, job.id)}
                          disabled={cancelingJobID === job.id}
                          className="rounded-lg border border-rose-200 px-3 py-1.5 text-xs font-semibold text-rose-700 transition hover:border-rose-300 hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-60"
                        >
                          {cancelingJobID === job.id ? "取消中..." : "取消任务"}
                        </button>
                      ) : null}
                      <span className="text-xs font-semibold text-slate-600">{formatStatus(job.status, job.draft_expired, job.error_message)}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
            {currentJobsState.loadMoreError ? <p className="mt-3 text-sm text-rose-600">{currentJobsState.loadMoreError}</p> : null}
            {currentJobsState.nextCursor ? (
              <div className="mt-4 flex justify-center">
                <button
                  type="button"
                  onClick={() => void loadMoreJobs()}
                  disabled={currentJobsState.loadingMore}
                  className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-medium text-slate-700 transition hover:border-primary/40 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {currentJobsState.loadingMore ? "加载中..." : "加载更多"}
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>
    </div>
  );
}
