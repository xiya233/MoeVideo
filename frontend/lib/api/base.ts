const DEFAULT_API_BASE = "http://localhost:8080/api/v1";

type RuntimeEnv = {
  NEXT_PUBLIC_API_BASE_URL?: string;
};

declare global {
  interface Window {
    __MOEVIDEO_RUNTIME__?: RuntimeEnv;
  }
}

function normalizeApiBase(value: string | undefined | null): string | null {
  const trimmed = (value ?? "").trim();
  if (!trimmed) {
    return null;
  }
  return trimmed.replace(/\/+$/, "");
}

export function getApiBase(): string {
  if (typeof window !== "undefined") {
    const runtimeBase = normalizeApiBase(window.__MOEVIDEO_RUNTIME__?.NEXT_PUBLIC_API_BASE_URL);
    if (runtimeBase) {
      return runtimeBase;
    }
  }

  const serverRuntimeBase = normalizeApiBase(process.env.API_BASE_URL);
  if (serverRuntimeBase) {
    return serverRuntimeBase;
  }

  const buildTimeBase = normalizeApiBase(process.env.NEXT_PUBLIC_API_BASE_URL);
  return buildTimeBase ?? DEFAULT_API_BASE;
}

