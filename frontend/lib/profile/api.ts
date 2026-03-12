import type { RequestOptions } from "@/lib/api/types";
import type { UserBrief, UserProfileData, VideoCard } from "@/lib/dto";

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

export const profileApi = {
  getProfile(request: ApiRequest, userID: string) {
    return request<UserProfileData>(`/users/${userID}`, { auth: true });
  },

  listVideos(request: ApiRequest, userID: string, params: { cursor?: string; limit?: number }) {
    return request<CursorList<VideoCard>>(`/users/${userID}/videos${buildQuery(params)}`, { auth: true });
  },

  listFavorites(request: ApiRequest, userID: string, params: { cursor?: string; limit?: number }) {
    return request<CursorList<VideoCard>>(`/users/${userID}/favorites${buildQuery(params)}`, { auth: true });
  },

  listFollowing(request: ApiRequest, userID: string, params: { cursor?: string; limit?: number }) {
    return request<CursorList<UserBrief>>(`/users/${userID}/following${buildQuery(params)}`, { auth: true });
  },

  listFollowers(request: ApiRequest, userID: string, params: { cursor?: string; limit?: number }) {
    return request<CursorList<UserBrief>>(`/users/${userID}/followers${buildQuery(params)}`, { auth: true });
  },
};
