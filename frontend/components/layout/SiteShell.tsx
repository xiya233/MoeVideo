"use client";

import { usePathname } from "next/navigation";
import { Suspense, useEffect, type ReactNode } from "react";

import { AuthDialog } from "@/components/auth/AuthDialog";
import { AppIcon } from "@/components/common/AppIcon";
import { SiteHeader } from "@/components/layout/SiteHeader";
import { usePublicSiteSettings } from "@/lib/site-settings/public";

function BrandMark({ siteTitle, siteLogoURL, size = 36 }: { siteTitle: string; siteLogoURL?: string; size?: number }) {
  if (siteLogoURL) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={siteLogoURL} alt={siteTitle} className="rounded-md object-cover" style={{ width: size, height: size }} />;
  }
  return <AppIcon name="face_5" size={size} />;
}

function HomeFooter({
  siteTitle,
  siteDescription,
  siteLogoURL,
}: {
  siteTitle: string;
  siteDescription: string;
  siteLogoURL?: string;
}) {
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

        <div>
          <h4 className="mb-4 text-sm font-bold">关于我们</h4>
          <ul className="space-y-2 text-xs font-medium text-slate-500">
            <li>加入我们</li>
            <li>联系我们</li>
            <li>创作团队</li>
          </ul>
        </div>

        <div>
          <h4 className="mb-4 text-sm font-bold">帮助支持</h4>
          <ul className="space-y-2 text-xs font-medium text-slate-500">
            <li>反馈中心</li>
            <li>隐私设置</li>
            <li>上传规范</li>
          </ul>
        </div>

        <div>
          <h4 className="mb-4 text-sm font-bold">协议条款</h4>
          <ul className="space-y-2 text-xs font-medium text-slate-500">
            <li>用户协议</li>
            <li>隐私政策</li>
            <li>版权声明</li>
          </ul>
        </div>

        <div>
          <h4 className="mb-4 text-sm font-bold">关注更新</h4>
          <ul className="space-y-2 text-xs font-medium text-slate-500">
            <li>官方公告</li>
            <li>活动中心</li>
            <li>开发日志</li>
          </ul>
        </div>
      </div>
    </footer>
  );
}

function CompactFooter({
  uploadPage,
  siteTitle,
  siteLogoURL,
}: {
  uploadPage?: boolean;
  siteTitle: string;
  siteLogoURL?: string;
}) {
  return (
    <footer className="border-t border-primary/10 bg-white py-10">
      <div className="mx-auto w-full max-w-[1440px] px-4 text-center md:px-10">
        {uploadPage ? (
          <p className="text-xs text-slate-400">© 2026 {siteTitle} · 为创作而生</p>
        ) : (
          <>
            <div className="mb-6 flex items-center justify-center gap-2 text-primary">
              {siteLogoURL ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={siteLogoURL} alt={siteTitle} className="h-6 w-6 rounded-md object-cover" />
              ) : (
                <AppIcon name="play_circle" size={24} />
              )}
              <span className="text-lg font-bold">{siteTitle}</span>
            </div>
            <p className="text-sm text-slate-500">© 2026 {siteTitle} Project. Designed for anime and scenery lovers.</p>
            <div className="mt-6 flex items-center justify-center gap-6 text-slate-400">
              <AppIcon name="alternate_email" className="transition-colors hover:text-primary" />
              <AppIcon name="public" className="transition-colors hover:text-primary" />
              <AppIcon name="groups" className="transition-colors hover:text-primary" />
            </div>
          </>
        )}
      </div>
    </footer>
  );
}

function SiteFooter({
  siteTitle,
  siteDescription,
  siteLogoURL,
}: {
  siteTitle: string;
  siteDescription: string;
  siteLogoURL?: string;
}) {
  const pathname = usePathname();
  if (pathname === "/") {
    return <HomeFooter siteTitle={siteTitle} siteDescription={siteDescription} siteLogoURL={siteLogoURL} />;
  }
  return <CompactFooter uploadPage={pathname === "/upload"} siteTitle={siteTitle} siteLogoURL={siteLogoURL} />;
}

export function SiteShell({ children }: { children: ReactNode }) {
  const siteSettingsQuery = usePublicSiteSettings();
  const siteTitle = siteSettingsQuery.data?.site_title?.trim() || "MoeVideo";
  const siteDescription = siteSettingsQuery.data?.site_description?.trim() || "MoeVideo VOD - Stitch design implementation";
  const siteLogoURL = siteSettingsQuery.data?.site_logo_url;

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
        <SiteFooter siteTitle={siteTitle} siteDescription={siteDescription} siteLogoURL={siteLogoURL} />
      </Suspense>
      <AuthDialog />
    </div>
  );
}
