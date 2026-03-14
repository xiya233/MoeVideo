"use client";

import Link from "next/link";
import { Suspense, useEffect, useState, type ReactNode } from "react";

import { AuthDialog } from "@/components/auth/AuthDialog";
import { AppIcon } from "@/components/common/AppIcon";
import { SiteHeader } from "@/components/layout/SiteHeader";
import { usePublicSiteSettings } from "@/lib/site-settings/public";
import type { FooterLinks } from "@/lib/site-settings/types";

function BrandMark({ siteTitle, siteLogoURL, size = 36 }: { siteTitle: string; siteLogoURL?: string; size?: number }) {
  if (siteLogoURL) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={siteLogoURL} alt={siteTitle} className="rounded-md object-cover" style={{ width: size, height: size }} />;
  }
  return <AppIcon name="face_5" size={size} />;
}

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

function normalizeFooterLinks(footerLinks?: FooterLinks): FooterLinks {
  const valid = footerLinks ?? DEFAULT_FOOTER_LINKS;
  const pickGroup = (value: unknown, fallback: FooterLinks[keyof FooterLinks]) => {
    if (!Array.isArray(value) || value.length !== 3) {
      return fallback;
    }
    const mapped = value
      .map((item) => {
        if (typeof item !== "object" || item === null) {
          return null;
        }
        const row = item as { label?: unknown; url?: unknown };
        const label = typeof row.label === "string" ? row.label.trim() : "";
        const url = typeof row.url === "string" ? row.url.trim() : "";
        if (!label || !url) {
          return null;
        }
        return { label, url };
      })
      .filter((item): item is { label: string; url: string } => item !== null);
    return mapped.length === 3 ? mapped : fallback;
  };

  return {
    about: pickGroup(valid.about, DEFAULT_FOOTER_LINKS.about),
    support: pickGroup(valid.support, DEFAULT_FOOTER_LINKS.support),
    legal: pickGroup(valid.legal, DEFAULT_FOOTER_LINKS.legal),
    updates: pickGroup(valid.updates, DEFAULT_FOOTER_LINKS.updates),
  };
}

function isInternalFooterURL(url: string): boolean {
  return url.startsWith("/");
}

function HomeFooter({
  siteTitle,
  siteDescription,
  siteLogoURL,
  footerLinks,
}: {
  siteTitle: string;
  siteDescription: string;
  siteLogoURL?: string;
  footerLinks: FooterLinks;
}) {
  const links = normalizeFooterLinks(footerLinks);
  const sections: Array<{ key: keyof FooterLinks; title: string }> = [
    { key: "about", title: "关于我们" },
    { key: "support", title: "帮助支持" },
    { key: "legal", title: "协议条款" },
    { key: "updates", title: "关注更新" },
  ];

  return (
    <footer className="border-t border-slate-100 bg-white px-4 py-12 md:px-10">
      <div className="mx-auto grid w-full max-w-[1400px] grid-cols-2 gap-8 md:grid-cols-4 lg:grid-cols-6">
        <div className="col-span-2">
          <div className="mb-6 flex items-center gap-2 text-primary">
            <BrandMark siteTitle={siteTitle} siteLogoURL={siteLogoURL} />
            <h2 className="text-xl font-bold tracking-tight">{siteTitle}</h2>
          </div>
          <p className="mb-6 max-w-xs text-sm text-slate-500">
            {siteDescription || "发现最萌最有趣的二次元视频内容，与志同道合的小伙伴一起分享快乐。"}
          </p>
          <div className="flex gap-4 text-primary">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <AppIcon name="public" size={18} />
            </div>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <AppIcon name="alternate_email" size={18} />
            </div>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <AppIcon name="chat" size={18} />
            </div>
          </div>
        </div>

        {sections.map((section) => (
          <div key={section.key}>
            <h4 className="mb-4 text-sm font-bold">{section.title}</h4>
            <ul className="space-y-2 text-xs font-medium text-slate-500">
              {links[section.key].map((item) => (
                <li key={`${section.key}-${item.label}-${item.url}`}>
                  {isInternalFooterURL(item.url) ? (
                    <Link href={item.url} className="transition-colors hover:text-primary">
                      {item.label}
                    </Link>
                  ) : (
                    <a href={item.url} target="_blank" rel="noopener noreferrer" className="transition-colors hover:text-primary">
                      {item.label}
                    </a>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </footer>
  );
}

function SiteFooter({
  siteTitle,
  siteDescription,
  siteLogoURL,
  footerLinks,
}: {
  siteTitle: string;
  siteDescription: string;
  siteLogoURL?: string;
  footerLinks: FooterLinks;
}) {
  return <HomeFooter siteTitle={siteTitle} siteDescription={siteDescription} siteLogoURL={siteLogoURL} footerLinks={footerLinks} />;
}

export function SiteShell({ children }: { children: ReactNode }) {
  const [mounted, setMounted] = useState(false);
  const siteSettingsQuery = usePublicSiteSettings();
  const siteTitle = siteSettingsQuery.data?.site_title?.trim() || "MoeVideo";
  const siteDescription = siteSettingsQuery.data?.site_description?.trim() || "MoeVideo VOD - Stitch design implementation";
  const siteLogoURL = siteSettingsQuery.data?.site_logo_url;
  const footerLinks = normalizeFooterLinks(siteSettingsQuery.data?.footer_links);
  const displayedSiteTitle = mounted ? siteTitle : "MoeVideo";
  const displayedSiteDescription = mounted ? siteDescription : "MoeVideo VOD - Stitch design implementation";
  const displayedSiteLogoURL = mounted ? siteLogoURL : undefined;

  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    document.title = siteTitle;
    let meta = document.querySelector('meta[name="description"]');
    if (!meta) {
      meta = document.createElement("meta");
      meta.setAttribute("name", "description");
      document.head.appendChild(meta);
    }
    meta.setAttribute("content", siteDescription);
  }, [siteDescription, siteTitle]);

  return (
    <div className="min-h-screen bg-background-light text-slate-900">
      <Suspense fallback={<div className="h-[73px] border-b border-primary/10 bg-white/80" />}>
        <SiteHeader />
      </Suspense>
      <main className="mx-auto w-full max-w-[1400px] px-4 py-8 md:px-10">{children}</main>
      <Suspense>
        <SiteFooter
          siteTitle={displayedSiteTitle}
          siteDescription={displayedSiteDescription}
          siteLogoURL={displayedSiteLogoURL}
          footerLinks={footerLinks}
        />
      </Suspense>
      <AuthDialog />
    </div>
  );
}
