import type { RequestOptions } from "@/lib/api/types";
import type { ContinueWatchingItem, UserBrief, UserYTDLPCookieProfile, VideoCard, VideoDetail } from "@/lib/dto";

export type ApiRequest = <T>(path: string, options?: RequestOptions) => Promise<T>;

export type CursorList<T> = {
  items: T[];
  next_cursor?: string;
};

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

export const meApi = {
  listMyVideos(request: ApiRequest, params: { cursor?: string; limit?: number }) {
    return request<CursorList<VideoCard>>(`/users/me/videos${buildQuery(params)}`, { auth: true });
  },

  listMyFavorites(request: ApiRequest, params: { cursor?: string; limit?: number }) {
    return request<CursorList<VideoCard>>(`/users/me/favorites${buildQuery(params)}`, { auth: true });
  },

  listMyFollowing(request: ApiRequest, params: { cursor?: string; limit?: number }) {
    return request<CursorList<UserBrief>>(`/users/me/following${buildQuery(params)}`, { auth: true });
  },

  listMyFollowers(request: ApiRequest, params: { cursor?: string; limit?: number }) {
    return request<CursorList<UserBrief>>(`/users/me/followers${buildQuery(params)}`, { auth: true });
  },

  listContinueWatching(request: ApiRequest, params: { cursor?: string; limit?: number }) {
    return request<CursorList<ContinueWatchingItem>>(`/users/me/continue-watching${buildQuery(params)}`, {
      auth: true,
    });
  },

  updateProfile(
    request: ApiRequest,
    payload: {
      bio?: string;
      avatar_media_id?: string;
      profile_public?: boolean;
      public_videos?: boolean;
      public_favorites?: boolean;
      public_following?: boolean;
      public_followers?: boolean;
    },
  ) {
    return request<{ user: UserBrief & { email?: string; role?: string } }>(`/users/me`, {
      method: "PATCH",
      auth: true,
      body: payload,
    });
  },

  getMyVideoDetail(request: ApiRequest, videoId: string) {
    return request<VideoDetail>(`/videos/${videoId}`, { auth: true });
  },

  updateMyVideo(
    request: ApiRequest,
    videoId: string,
    payload: {
      title?: string;
      description?: string;
      visibility?: "public" | "unlisted" | "private";
      tags?: string[];
    },
  ) {
    return request<{ updated: boolean }>(`/videos/${videoId}`, {
      method: "PATCH",
      auth: true,
      body: payload,
    });
  },

  deleteMyVideo(request: ApiRequest, videoId: string) {
    return request<{ deleted: boolean; cleanup_warnings?: string[] }>(`/videos/${videoId}`, {
      method: "DELETE",
      auth: true,
    });
  },

  listMyYTDLPCookies(request: ApiRequest, params?: { for_url?: string }) {
    return request<{ items: UserYTDLPCookieProfile[] }>(`/users/me/ytdlp-cookies${buildQuery(params ?? {})}`, {
      auth: true,
    });
  },

  createMyYTDLPCookie(
    request: ApiRequest,
    payload: {
      label: string;
      domain_rule: string;
      format: "header" | "cookies_txt";
      content: string;
    },
  ) {
    return request<UserYTDLPCookieProfile>(`/users/me/ytdlp-cookies`, {
      method: "POST",
      auth: true,
      body: payload,
    });
  },

  updateMyYTDLPCookie(
    request: ApiRequest,
    cookieId: string,
    payload: {
      label?: string;
      domain_rule?: string;
      format?: "header" | "cookies_txt";
      content?: string;
    },
  ) {
    return request<UserYTDLPCookieProfile>(`/users/me/ytdlp-cookies/${cookieId}`, {
      method: "PATCH",
      auth: true,
      body: payload,
    });
  },

  deleteMyYTDLPCookie(request: ApiRequest, cookieId: string) {
    return request<{ deleted: boolean; id: string }>(`/users/me/ytdlp-cookies/${cookieId}`, {
      method: "DELETE",
      auth: true,
    });
  },
};
