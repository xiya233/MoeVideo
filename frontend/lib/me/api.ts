import type { RequestOptions } from "@/lib/api/types";
import type { ContinueWatchingItem, UserBrief, VideoCard } from "@/lib/dto";

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
};
