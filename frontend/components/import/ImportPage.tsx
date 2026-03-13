"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

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

const POLL_INTERVAL = 2000;
const MAX_PREVIEW_JOBS = 10;

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

function formatStatus(status: ImportJob["status"]): string {
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

function isJobActive(status: ImportJob["status"]): boolean {
  return status === "draft" || status === "queued" || status === "downloading";
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

export function ImportPage() {
  const { ready, session, openAuthDialog, request } = useAuth();

  const [categories, setCategories] = useState<Category[]>([]);
  const [loadingCategories, setLoadingCategories] = useState(false);

  const [torrentFile, setTorrentFile] = useState<File | null>(null);
  const [inspectJob, setInspectJob] = useState<ImportJob | null>(null);
  const [inspectItems, setInspectItems] = useState<ImportItem[]>([]);
  const [selectedIndexes, setSelectedIndexes] = useState<Set<number>>(new Set());

  const [categoryID, setCategoryID] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private" | "unlisted">("public");
  const [tagInput, setTagInput] = useState("");

  const [inspectPending, setInspectPending] = useState(false);
  const [startPending, setStartPending] = useState(false);
  const [inspectError, setInspectError] = useState("");
  const [startError, setStartError] = useState("");

  const [activeJobID, setActiveJobID] = useState("");
  const [activeJobDetail, setActiveJobDetail] = useState<ImportJobDetailData | null>(null);
  const [activeJobError, setActiveJobError] = useState("");

  const [jobs, setJobs] = useState<ImportJob[]>([]);
  const [jobsError, setJobsError] = useState("");

  const pollRef = useRef<number | null>(null);

  const selectedCount = selectedIndexes.size;

  const stopPolling = useCallback(() => {
    if (pollRef.current !== null) {
      window.clearInterval(pollRef.current);
      pollRef.current = null;
    }
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

  const loadJobs = useCallback(async () => {
    if (!session) {
      setJobs([]);
      return;
    }
    setJobsError("");
    try {
      const dataRaw = await importsApi.listJobs(request, { limit: MAX_PREVIEW_JOBS });
      const data = mapImportJobsData(dataRaw as ImportJobsData);
      setJobs(data.items);
    } catch (err) {
      setJobs([]);
      setJobsError(err instanceof Error ? err.message : "任务列表加载失败");
    }
  }, [request, session]);

  const loadJobDetail = useCallback(
    async (jobID: string) => {
      const dataRaw = await importsApi.getJob(request, jobID);
      const data = mapImportJobDetailData(dataRaw);
      setActiveJobDetail(data);
      setActiveJobError("");
      return data;
    },
    [request],
  );

  useEffect(() => {
    if (!session) {
      return;
    }
    void loadCategories();
    void loadJobs();
  }, [loadCategories, loadJobs, session]);

  useEffect(() => {
    return () => {
      stopPolling();
    };
  }, [stopPolling]);

  const openPolling = useCallback(
    (jobID: string) => {
      stopPolling();
      pollRef.current = window.setInterval(() => {
        void (async () => {
          try {
            const next = await loadJobDetail(jobID);
            if (!isJobActive(next.job.status)) {
              stopPolling();
              await loadJobs();
            }
          } catch (err) {
            setActiveJobError(err instanceof Error ? err.message : "导入任务刷新失败");
          }
        })();
      }, POLL_INTERVAL);
    },
    [loadJobDetail, loadJobs, stopPolling],
  );

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
      setSelectedIndexes(new Set(mappedItems.map((item) => item.file_index)));
      setActiveJobID(result.job.id);
      const detail = await loadJobDetail(result.job.id);
      if (isJobActive(detail.job.status)) {
        openPolling(result.job.id);
      } else {
        stopPolling();
      }
      await loadJobs();
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
    if (selectedIndexes.size === 0) {
      setStartError("至少勾选一个视频文件");
      return;
    }

    setStartPending(true);
    setStartError("");

    try {
      await importsApi.startTorrent(request, {
        job_id: inspectJob.id,
        selected_file_indexes: Array.from(selectedIndexes).sort((a, b) => a - b),
        category_id: categoryID ? Number(categoryID) : undefined,
        tags: normalizeTags(tagInput),
        visibility,
      });

      setActiveJobID(inspectJob.id);
      const detail = await loadJobDetail(inspectJob.id);
      if (isJobActive(detail.job.status)) {
        openPolling(inspectJob.id);
      }
      await loadJobs();
    } catch (err) {
      setStartError(err instanceof Error ? err.message : "开始导入失败");
    } finally {
      setStartPending(false);
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

  const activeDetail = useMemo(() => {
    if (!activeJobDetail) {
      return null;
    }
    if (activeJobID && activeJobDetail.job.id === activeJobID) {
      return activeJobDetail;
    }
    return activeJobDetail;
  }, [activeJobDetail, activeJobID]);

  if (!ready) {
    return (
      <div className="mx-auto w-full max-w-6xl rounded-xl border border-primary/10 bg-white p-10 shadow-sm">
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
      <div className="mx-auto w-full max-w-6xl rounded-xl border border-primary/10 bg-white p-10 shadow-sm">
        <EmptyState title="请先登录后再导入" description="导入视频需要登录态，登录后可提交 BT 种子任务。" />
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
    <div className="mx-auto w-full max-w-6xl space-y-6">
      <section className="rounded-2xl border border-primary/10 bg-white p-6 shadow-sm md:p-8">
        <div className="mb-4 flex items-center gap-2">
          <AppIcon name="input" className="text-primary" />
          <h1 className="text-xl font-bold text-slate-900">导入视频</h1>
        </div>
        <p className="text-sm text-slate-500">上传 BT 种子后先解析文件列表，再勾选要导入的视频，系统会自动转码发布。</p>

        <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-[1fr_auto]">
          <label className="flex cursor-pointer items-center gap-3 rounded-xl border border-dashed border-primary/30 bg-primary/5 px-4 py-3 text-sm text-slate-700 transition hover:bg-primary/10">
            <AppIcon name="upload" size={18} className="text-primary" />
            <span className="truncate">{torrentFile ? torrentFile.name : "选择 .torrent 文件"}</span>
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
            className="rounded-xl bg-primary px-5 py-3 text-sm font-bold text-white transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {inspectPending ? "解析中..." : "解析种子"}
          </button>
        </div>

        {inspectError ? <p className="mt-3 text-sm text-rose-600">{inspectError}</p> : null}
      </section>

      <section className="rounded-2xl border border-primary/10 bg-white p-6 shadow-sm md:p-8">
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
              <label className="space-y-1">
                <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">分类</span>
                <select
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                  value={categoryID}
                  onChange={(event) => setCategoryID(event.target.value)}
                  disabled={loadingCategories}
                >
                  <option value="">不指定</option>
                  {categories.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>

              <label className="space-y-1">
                <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">可见性</span>
                <select
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
                  value={visibility}
                  onChange={(event) => setVisibility(event.target.value as "public" | "private" | "unlisted")}
                >
                  <option value="public">公开</option>
                  <option value="unlisted">非公开列表</option>
                  <option value="private">仅自己可见</option>
                </select>
              </label>

              <label className="space-y-1 md:col-span-1">
                <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">标签（逗号分隔）</span>
                <input
                  value={tagInput}
                  onChange={(event) => setTagInput(event.target.value)}
                  placeholder="例如：动漫,二创"
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/40"
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
                disabled={startPending || inspectItems.length === 0}
                className="rounded-xl bg-primary px-5 py-2 text-sm font-bold text-white transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {startPending ? "提交中..." : "开始导入"}
              </button>
            </div>
          </>
        )}
      </section>

      <section className="rounded-2xl border border-primary/10 bg-white p-6 shadow-sm md:p-8">
        <div className="mb-4 flex items-center justify-between gap-3">
          <h2 className="text-lg font-bold text-slate-900">导入任务进度</h2>
          {activeDetail ? (
            <span
              className={cn(
                "rounded-full px-3 py-1 text-xs font-semibold",
                activeDetail.job.status === "succeeded"
                  ? "bg-emerald-100 text-emerald-700"
                  : activeDetail.job.status === "partial"
                    ? "bg-amber-100 text-amber-700"
                    : activeDetail.job.status === "failed"
                      ? "bg-rose-100 text-rose-700"
                      : "bg-sky-100 text-sky-700",
              )}
            >
              {formatStatus(activeDetail.job.status)}
            </span>
          ) : null}
        </div>

        {activeJobError ? <p className="mb-3 text-sm text-rose-600">{activeJobError}</p> : null}

        {!activeDetail ? (
          <EmptyState title="暂无活跃任务" description="解析并开始导入后，这里会显示实时进度。" />
        ) : (
          <>
            <div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
              <p className="text-sm font-medium text-slate-700">{activeDetail.job.source_filename}</p>
              <p className="mt-1 text-xs text-slate-500">InfoHash: {activeDetail.job.info_hash}</p>
              <div className="mt-3 h-2 overflow-hidden rounded-full bg-slate-200">
                <div className="h-full rounded-full bg-primary" style={{ width: `${Math.min(100, Math.max(0, activeDetail.job.progress))}%` }} />
              </div>
              <p className="mt-2 text-xs text-slate-500">
                已完成 {activeDetail.job.completed_files}/{activeDetail.job.selected_files} · 失败 {activeDetail.job.failed_files}
              </p>
              {activeDetail.job.error_message ? <p className="mt-2 text-xs text-rose-600">{activeDetail.job.error_message}</p> : null}
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

      <section className="rounded-2xl border border-primary/10 bg-white p-6 shadow-sm md:p-8">
        <h2 className="mb-4 text-lg font-bold text-slate-900">最近导入任务</h2>
        {jobsError ? <p className="mb-3 text-sm text-rose-600">{jobsError}</p> : null}

        {jobs.length === 0 ? (
          <EmptyState title="暂无导入记录" description="导入任务提交后会显示在这里。" />
        ) : (
          <div className="space-y-3">
            {jobs.map((job) => (
              <button
                key={job.id}
                type="button"
                className="flex w-full items-center justify-between rounded-xl border border-slate-100 bg-slate-50 px-4 py-3 text-left transition hover:border-primary/40"
                onClick={() => {
                  setActiveJobID(job.id);
                  void loadJobDetail(job.id);
                  if (isJobActive(job.status)) {
                    openPolling(job.id);
                  } else {
                    stopPolling();
                  }
                }}
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-slate-800">{job.source_filename}</p>
                  <p className="mt-1 text-xs text-slate-500">
                    {job.completed_files}/{job.selected_files} 完成 · 失败 {job.failed_files}
                  </p>
                </div>
                <span className="text-xs font-semibold text-slate-600">{formatStatus(job.status)}</span>
              </button>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
