"use client";

import { usePathname } from "next/navigation";
import { Suspense, type ReactNode } from "react";

import { AuthDialog } from "@/components/auth/AuthDialog";
import { SiteHeader } from "@/components/layout/SiteHeader";

function HomeFooter() {
  return (
    <footer className="border-t border-slate-100 bg-white px-4 py-12 md:px-10">
      <div className="mx-auto grid w-full max-w-[1400px] grid-cols-2 gap-8 md:grid-cols-4 lg:grid-cols-6">
        <div className="col-span-2">
          <div className="mb-6 flex items-center gap-2 text-primary">
            <span className="material-symbols-outlined text-4xl" style={{ fontVariationSettings: "'FILL' 1" }}>
              face_5
            </span>
            <h2 className="text-xl font-bold tracking-tight">MoeVideo</h2>
          </div>
          <p className="mb-6 max-w-xs text-sm text-slate-500">
            发现最萌最有趣的二次元视频内容，与志同道合的小伙伴一起分享快乐。
          </p>
          <div className="flex gap-4 text-primary">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <span className="material-symbols-outlined text-lg">public</span>
            </div>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <span className="material-symbols-outlined text-lg">alternate_email</span>
            </div>
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10">
              <span className="material-symbols-outlined text-lg">chat</span>
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

function CompactFooter({ uploadPage }: { uploadPage?: boolean }) {
  return (
    <footer className="border-t border-primary/10 bg-white py-10">
      <div className="mx-auto w-full max-w-[1440px] px-4 text-center md:px-10">
        {uploadPage ? (
          <p className="text-xs text-slate-400">© 2024 喵影上传平台 · 为创作而生</p>
        ) : (
          <>
            <div className="mb-6 flex items-center justify-center gap-2 text-primary">
              <span className="material-symbols-outlined text-2xl">play_circle</span>
              <span className="text-lg font-bold">MoeVideo</span>
            </div>
            <p className="text-sm text-slate-500">© 2026 MoeVideo Project. Designed for anime and scenery lovers.</p>
            <div className="mt-6 flex items-center justify-center gap-6 text-slate-400">
              <span className="material-symbols-outlined transition-colors hover:text-primary">alternate_email</span>
              <span className="material-symbols-outlined transition-colors hover:text-primary">public</span>
              <span className="material-symbols-outlined transition-colors hover:text-primary">groups</span>
            </div>
          </>
        )}
      </div>
    </footer>
  );
}

function SiteFooter() {
  const pathname = usePathname();
  if (pathname === "/") {
    return <HomeFooter />;
  }
  return <CompactFooter uploadPage={pathname === "/upload"} />;
}

export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-background-light text-slate-900">
      <Suspense fallback={<div className="h-[73px] border-b border-primary/10 bg-white/80" />}>
        <SiteHeader />
      </Suspense>
      <main className="mx-auto w-full max-w-[1400px] px-4 py-8 md:px-10">{children}</main>
      <Suspense>
        <SiteFooter />
      </Suspense>
      <AuthDialog />
    </div>
  );
}
