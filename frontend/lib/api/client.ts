import { ApiError, type ApiEnvelope, type RequestContext, type RequestOptions } from "./types";
import { getApiBase } from "./base";

function asJsonBody(body: unknown): string {
  return JSON.stringify(body ?? {});
}

async function parseEnvelope<T>(res: Response): Promise<ApiEnvelope<T>> {
  const contentType = res.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    if (!res.ok) {
      throw new ApiError(`HTTP ${res.status}`, res.status);
    }
    return { code: 0, message: "ok", data: (null as T) };
  }

  const parsed = (await res.json()) as ApiEnvelope<T>;
  if (!res.ok || parsed.code !== 0) {
    throw new ApiError(parsed.message || `HTTP ${res.status}`, res.status, parsed.code);
  }
  return parsed;
}

export function createApiClient(ctx: RequestContext) {
  let refreshPromise: Promise<boolean> | null = null;

  const doRefresh = async (): Promise<boolean> => {
    if (refreshPromise) {
      return refreshPromise;
    }

    refreshPromise = (async () => {
      try {
        const res = await fetch(`${getApiBase()}/auth/refresh`, {
          method: "POST",
          credentials: "include",
        });
        await parseEnvelope<{ refreshed: boolean }>(res);
        return true;
      } catch {
        ctx.onAuthFailed();
        return false;
      } finally {
        refreshPromise = null;
      }
    })();

    return refreshPromise;
  };

  const request = async <T>(path: string, options: RequestOptions = {}): Promise<T> => {
    const method = options.method ?? "GET";
    const headers: Record<string, string> = {
      ...(options.body !== undefined ? { "Content-Type": "application/json" } : {}),
      ...(options.headers ?? {}),
    };

    try {
      const apiBase = getApiBase();
      const res = await fetch(`${apiBase}${path}`, {
        method,
        headers,
        body: options.body !== undefined ? asJsonBody(options.body) : undefined,
        cache: "no-store",
        credentials: "include",
      });

      const envelope = await parseEnvelope<T>(res);
      return envelope.data;
    } catch (error) {
      if (!(error instanceof ApiError)) {
        throw error;
      }

      const shouldTryRefresh =
        options.auth !== false &&
        !options.skipRefresh &&
        error.status === 401 &&
        !path.startsWith("/auth/");

      if (!shouldTryRefresh) {
        throw error;
      }

      const refreshed = await doRefresh();
      if (!refreshed) {
        throw error;
      }

      return request<T>(path, { ...options, skipRefresh: true });
    }
  };

  const uploadBinary = async (
    url: string,
    file: File,
    headers?: Record<string, string>,
  ): Promise<void> => {
    const res = await fetch(url, {
      method: "PUT",
      body: file,
      headers,
    });
    if (!res.ok) {
      throw new ApiError(`upload failed with status ${res.status}`, res.status);
    }
  };

  return { request, uploadBinary };
}
