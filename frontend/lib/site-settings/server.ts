import type { ApiEnvelope } from "@/lib/api/types";
import { getApiBase } from "@/lib/api/base";
import type { FooterLinks, PublicSiteSettings } from "@/lib/site-settings/types";

const DEFAULT_FOOTER_LINKS: FooterLinks = {
  about: [
    { label: "加入我们", url: "/about/join" },
    { label: "联系我们", url: "/about/contact" },
    { label: "创作团队", url: "/about/team" },
  ],
  support: [
    { label: "反馈中心", url: "/support/feedback" },
    { label: "隐私设置", url: "/support/privacy" },
    { label: "上传规范", url: "/support/upload-guidelines" },
  ],
  legal: [
    { label: "用户协议", url: "/legal/terms" },
    { label: "隐私政策", url: "/legal/privacy" },
    { label: "版权声明", url: "/legal/copyright" },
  ],
  updates: [
    { label: "官方公告", url: "/updates/news" },
    { label: "活动中心", url: "/updates/events" },
    { label: "开发日志", url: "/updates/changelog" },
  ],
};

const FALLBACK_PUBLIC_SITE_SETTINGS: PublicSiteSettings = {
  site_title: "MoeVideo",
  site_description: "MoeVideo VOD - Stitch design implementation",
  site_logo_url: "",
  footer_links: DEFAULT_FOOTER_LINKS,
  register_enabled: true,
};

function normalizePublicSiteSettings(raw?: Partial<PublicSiteSettings> | null): PublicSiteSettings {
  return {
    site_title: raw?.site_title?.trim() || FALLBACK_PUBLIC_SITE_SETTINGS.site_title,
    site_description: raw?.site_description?.trim() || FALLBACK_PUBLIC_SITE_SETTINGS.site_description,
    site_logo_url: raw?.site_logo_url ?? "",
    footer_links: raw?.footer_links ?? DEFAULT_FOOTER_LINKS,
    register_enabled:
      typeof raw?.register_enabled === "boolean"
        ? raw.register_enabled
        : FALLBACK_PUBLIC_SITE_SETTINGS.register_enabled,
  };
}

export async function getPublicSiteSettingsServer(): Promise<PublicSiteSettings> {
  try {
    const res = await fetch(`${getApiBase()}/site-settings/public`, {
      method: "GET",
      cache: "no-store",
    });
    const parsed = (await res.json()) as ApiEnvelope<PublicSiteSettings>;
    if (!res.ok || parsed.code !== 0 || !parsed.data) {
      return FALLBACK_PUBLIC_SITE_SETTINGS;
    }
    return normalizePublicSiteSettings(parsed.data);
  } catch {
    return FALLBACK_PUBLIC_SITE_SETTINGS;
  }
}
