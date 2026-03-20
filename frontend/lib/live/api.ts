import type { RequestOptions } from "@/lib/api/types";
import type { CreateLiveSessionData, LiveSessionData } from "@/lib/dto";

export type ApiRequest = <T>(path: string, options?: RequestOptions) => Promise<T>;

export const liveApi = {
  createSession(
    request: ApiRequest,
    payload: {
      title: string;
      description?: string;
      category_id: number;
      tags?: string[];
      visibility?: "public" | "private" | "unlisted";
    },
  ) {
    return request<CreateLiveSessionData>("/live/sessions", {
      method: "POST",
      auth: true,
      body: payload,
    });
  },

  getCurrentSession(request: ApiRequest) {
    return request<LiveSessionData>("/live/sessions/current", { auth: true });
  },

  endCurrentSession(request: ApiRequest) {
    return request<{ ended: boolean; session_id: string; video_id: string }>("/live/sessions/current/end", {
      method: "POST",
      auth: true,
    });
  },
};

