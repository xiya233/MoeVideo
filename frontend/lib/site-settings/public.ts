import { useQuery } from "@tanstack/react-query";

import { ApiError } from "@/lib/api/types";
import type { PublicSiteSettings } from "@/lib/site-settings/types";

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080/api/v1";

type Envelope<T> = {
  code: number;
  message: string;
  data: T;
};

async function fetchPublicSiteSettings(): Promise<PublicSiteSettings> {
  const res = await fetch(`${API_BASE}/site-settings/public`, {
    method: "GET",
    cache: "no-store",
  });
  const parsed = (await res.json()) as Envelope<PublicSiteSettings>;
  if (!res.ok || parsed.code !== 0) {
    throw new ApiError(parsed.message || `HTTP ${res.status}`, res.status, parsed.code);
  }
  return parsed.data;
}

type UsePublicSiteSettingsOptions = {
  initialData?: PublicSiteSettings;
};

export function usePublicSiteSettings(options?: UsePublicSiteSettingsOptions) {
  return useQuery({
    queryKey: ["site-settings-public"],
    queryFn: fetchPublicSiteSettings,
    initialData: options?.initialData,
  });
}
