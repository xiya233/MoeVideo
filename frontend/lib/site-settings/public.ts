import { useQuery } from "@tanstack/react-query";

import { getApiBase } from "@/lib/api/base";
import { ApiError } from "@/lib/api/types";
import type { PublicSiteSettings } from "@/lib/site-settings/types";

type Envelope<T> = {
  code: number;
  message: string;
  data: T;
};

async function fetchPublicSiteSettings(): Promise<PublicSiteSettings> {
  const res = await fetch(`${getApiBase()}/site-settings/public`, {
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
