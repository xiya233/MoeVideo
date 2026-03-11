"use client";

import { Suspense, type ReactNode } from "react";

import { AuthDialog } from "@/components/auth/AuthDialog";
import { SiteHeader } from "@/components/layout/SiteHeader";

export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen text-slate-900">
      <Suspense fallback={<div className="h-[73px] border-b border-sky-200/40 bg-[rgba(245,247,248,0.82)]" />}>
        <SiteHeader />
      </Suspense>
      <main className="mx-auto w-full max-w-[1400px] px-4 py-8 md:px-10">{children}</main>
      <footer className="border-t border-sky-100 bg-white/70 py-10">
        <div className="mx-auto flex w-full max-w-[1400px] items-center justify-between px-4 md:px-10">
          <p className="text-xs text-slate-500">© 2026 MoeVideo Project.</p>
          <p className="text-xs text-slate-400">Built with Next.js + Fiber</p>
        </div>
      </footer>
      <AuthDialog />
    </div>
  );
}
