"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { BarChart3, Clapperboard, FileText, MessageSquare, Repeat2, Settings2, Shield, Users } from "lucide-react";
import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils/cn";
import { useAuth } from "@/components/auth/AuthProvider";

const navItems = [
  { href: "/admin", label: "Dashboard", icon: BarChart3 },
  { href: "/admin/videos", label: "Videos", icon: Clapperboard },
  { href: "/admin/transcode", label: "Transcode", icon: Repeat2 },
  { href: "/admin/comments", label: "Comments", icon: MessageSquare },
  { href: "/admin/users", label: "Users", icon: Users },
  { href: "/admin/site-settings", label: "Site Settings", icon: Settings2 },
  { href: "/admin/audit-logs", label: "Audit Logs", icon: FileText },
];

export function AdminShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const { user, logout } = useAuth();

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <div className="mx-auto flex w-full max-w-[1600px]">
        <aside className="sticky top-0 hidden h-screen w-64 flex-col border-r border-slate-200 bg-white p-4 lg:flex">
          <div className="mb-8 flex items-center gap-2 px-2 pt-2 text-slate-900">
            <Shield className="h-5 w-5 text-primary" />
            <div>
              <p className="text-sm font-semibold">MoeVideo Admin</p>
              <p className="text-xs text-slate-500">一期管理面板</p>
            </div>
          </div>

          <nav className="space-y-1">
            {navItems.map(({ href, label, icon: Icon }) => {
              const active = pathname === href;
              return (
                <Link
                  key={href}
                  href={href}
                  className={cn(
                    "flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                    active ? "bg-primary/10 text-primary" : "text-slate-600 hover:bg-slate-100",
                  )}
                >
                  <Icon className="h-4 w-4" />
                  {label}
                </Link>
              );
            })}
          </nav>
        </aside>

        <div className="min-w-0 flex-1">
          <header className="sticky top-0 z-20 border-b border-slate-200 bg-white/95 px-4 py-3 backdrop-blur lg:px-8">
            <div className="flex items-center justify-between">
              <div>
                <h1 className="text-base font-semibold">后台管理</h1>
                <p className="text-xs text-slate-500">管理员：{user?.username ?? "-"}</p>
              </div>
              <Button variant="outline" size="sm" onClick={() => void logout()}>
                退出登录
              </Button>
            </div>
          </header>

          <main className="p-4 lg:p-8">{children}</main>
        </div>
      </div>
    </div>
  );
}
