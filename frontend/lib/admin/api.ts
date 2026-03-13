import type { RequestOptions } from "@/lib/api/types";
import type {
  AdminAuditLog,
  AdminComment,
  AdminOverview,
  AdminSiteCategory,
  AdminSiteSettings,
  AdminTranscodeJob,
  AdminUser,
  AdminVideoItem,
  CursorList,
} from "@/lib/admin/types";

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

export const adminApi = {
  getOverview(request: ApiRequest) {
    return request<AdminOverview>("/admin/overview", { auth: true });
  },

  getSiteSettings(request: ApiRequest) {
    return request<AdminSiteSettings>("/admin/site-settings", { auth: true });
  },

  patchSiteSettings(
    request: ApiRequest,
    payload: {
      site_title?: string;
      site_description?: string;
      site_logo_media_id?: string;
      register_enabled?: boolean;
      ytdlp_param_mode?: "safe" | "advanced";
      ytdlp_safe?: {
        format?: string;
        extractor_args?: string;
        user_agent?: string;
        referer?: string;
        headers?: Record<string, string>;
        socket_timeout?: number;
      };
      ytdlp_metadata_args_raw?: string;
      ytdlp_download_args_raw?: string;
    },
  ) {
    return request<AdminSiteSettings>("/admin/site-settings", {
      method: "PATCH",
      auth: true,
      body: payload,
    });
  },

  listSiteCategories(request: ApiRequest) {
    return request<{ items: AdminSiteCategory[] }>("/admin/site-settings/categories", { auth: true });
  },

  createSiteCategory(
    request: ApiRequest,
    payload: {
      slug: string;
      name: string;
      sort_order?: number;
      is_active?: boolean;
    },
  ) {
    return request<AdminSiteCategory>("/admin/site-settings/categories", {
      method: "POST",
      auth: true,
      body: payload,
    });
  },

  patchSiteCategory(
    request: ApiRequest,
    id: number,
    payload: {
      slug?: string;
      name?: string;
      sort_order?: number;
      is_active?: boolean;
    },
  ) {
    return request<AdminSiteCategory>(`/admin/site-settings/categories/${id}`, {
      method: "PATCH",
      auth: true,
      body: payload,
    });
  },

  deleteSiteCategory(request: ApiRequest, id: number) {
    return request<{ deleted: boolean; id: number }>(`/admin/site-settings/categories/${id}`, {
      method: "DELETE",
      auth: true,
    });
  },

  listVideos(
    request: ApiRequest,
    params: {
      q?: string;
      status?: string;
      visibility?: string;
      uploader_id?: string;
      category_id?: string;
      cursor?: string;
      limit?: number;
    },
  ) {
    return request<CursorList<AdminVideoItem>>(`/admin/videos${buildQuery(params)}`, { auth: true });
  },

  getVideo(request: ApiRequest, id: string) {
    return request<Record<string, unknown>>(`/admin/videos/${id}`, { auth: true });
  },

  videoAction(request: ApiRequest, id: string, action: string) {
    return request<{ applied: boolean; action: string }>(`/admin/videos/${id}/actions`, {
      method: "POST",
      auth: true,
      body: { action },
    });
  },

  listTranscodeJobs(
    request: ApiRequest,
    params: {
      status?: string;
      video_id?: string;
      cursor?: string;
      limit?: number;
    },
  ) {
    return request<CursorList<AdminTranscodeJob>>(`/admin/transcode-jobs${buildQuery(params)}`, { auth: true });
  },

  retryTranscodeJob(request: ApiRequest, jobId: string) {
    return request<{ queued: boolean; job_id: string; video_id: string }>(`/admin/transcode-jobs/${jobId}/retry`, {
      method: "POST",
      auth: true,
      body: {},
    });
  },

  listComments(
    request: ApiRequest,
    params: {
      q?: string;
      video_id?: string;
      user_id?: string;
      status?: string;
      cursor?: string;
      limit?: number;
    },
  ) {
    return request<CursorList<AdminComment>>(`/admin/comments${buildQuery(params)}`, { auth: true });
  },

  commentsAction(request: ApiRequest, action: "delete" | "restore", commentIds: string[]) {
    return request<{ action: string; affected: number }>(`/admin/comments/actions`, {
      method: "POST",
      auth: true,
      body: {
        action,
        comment_ids: commentIds,
      },
    });
  },

  listUsers(
    request: ApiRequest,
    params: {
      q?: string;
      status?: string;
      role?: string;
      cursor?: string;
      limit?: number;
    },
  ) {
    return request<CursorList<AdminUser>>(`/admin/users${buildQuery(params)}`, { auth: true });
  },

  patchUser(
    request: ApiRequest,
    id: string,
    payload: {
      status?: "active" | "disabled";
      role?: "user" | "admin";
    },
  ) {
    return request<{ id: string; role: string; status: string }>(`/admin/users/${id}`, {
      method: "PATCH",
      auth: true,
      body: payload,
    });
  },

  listAuditLogs(
    request: ApiRequest,
    params: {
      actor_id?: string;
      action?: string;
      resource_type?: string;
      from?: string;
      to?: string;
      cursor?: string;
      limit?: number;
    },
  ) {
    return request<CursorList<AdminAuditLog>>(`/admin/audit-logs${buildQuery(params)}`, { auth: true });
  },
};
