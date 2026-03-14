import type { RequestOptions } from "@/lib/api/types";
import type { ImportJobDetailData, ImportJobsData, TorrentInspectResult } from "@/lib/dto";

export type ApiRequest = <T>(path: string, options?: RequestOptions) => Promise<T>;

function buildQuery(params: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") {
      continue;
    }
    q.set(key, String(value));
  }
  const encoded = q.toString();
  return encoded ? `?${encoded}` : "";
}

export const importsApi = {
  inspectTorrent(request: ApiRequest, payload: { filename: string; torrent_base64: string }) {
    return request<TorrentInspectResult>("/imports/torrent/inspect", {
      method: "POST",
      auth: true,
      body: payload,
    });
  },

  startTorrent(
    request: ApiRequest,
    payload: {
      job_id: string;
      selected_file_indexes: number[];
      category_id: number;
      tags?: string[];
      visibility?: "public" | "private" | "unlisted";
      title?: string;
      title_prefix?: string;
      description?: string;
    },
  ) {
    return request<{ job_id: string; status: "queued"; selected_files: number }>("/imports/torrent/start", {
      method: "POST",
      auth: true,
      body: payload,
    });
  },

  startURL(
    request: ApiRequest,
    payload: {
      url: string;
      category_id: number;
      tags?: string[];
      visibility?: "public" | "private" | "unlisted";
      title?: string;
      description?: string;
    },
  ) {
    return request<{ job_id: string; status: "queued"; selected_files: number }>("/imports/url/start", {
      method: "POST",
      auth: true,
      body: payload,
    });
  },

  listJobs(request: ApiRequest, params: { cursor?: string; limit?: number }) {
    return request<ImportJobsData>(`/imports${buildQuery(params)}`, { auth: true });
  },

  getJob(request: ApiRequest, jobId: string) {
    return request<ImportJobDetailData>(`/imports/${jobId}`, { auth: true });
  },
};
