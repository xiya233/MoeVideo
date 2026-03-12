"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon } from "@/components/common/AppIcon";
import { cn } from "@/lib/utils/cn";

export function SiteHeader() {
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();

  const { user, openAuthDialog, logout } = useAuth();

  const [searchValue, setSearchValue] = useState("");
  const [logoutPending, setLogoutPending] = useState(false);

  useEffect(() => {
    setSearchValue(searchParams.get("q") ?? "");
  }, [searchParams]);

  const submitSearch = () => {
    const keyword = searchValue.trim();
    const query = keyword ? `?q=${encodeURIComponent(keyword)}` : "";
    router.push(`/${query}`);
  };

  const onLogout = async () => {
    setLogoutPending(true);
    try {
      await logout();
    } finally {
      setLogoutPending(false);
    }
  };

  return (
    <header className="sticky top-0 z-50 border-b border-primary/10 bg-white/80 px-4 py-3 backdrop-blur-md md:px-10">
      <div className="mx-auto flex w-full max-w-[1400px] items-center justify-between gap-4">
        <div className="flex items-center gap-8">
          <Link className="flex items-center gap-2 text-primary" href="/">
            <AppIcon name="face_5" size={36} />
            <h2 className="text-xl font-bold tracking-tight text-slate-900">MoeVideo</h2>
          </Link>

          <nav className="hidden items-center gap-6 md:flex">
            <Link
              href="/"
              className={cn(
                "flex items-center gap-1 text-sm font-medium text-slate-700 transition-colors hover:text-primary",
                pathname === "/" && "text-primary",
              )}
            >
              <AppIcon name="home" size={20} />
              首页
            </Link>
            <Link
              href="/#categories"
              className="flex items-center gap-1 text-sm font-medium text-slate-700 transition-colors hover:text-primary"
            >
              <AppIcon name="grid_view" size={20} />
              分类
            </Link>
          </nav>
        </div>

        <div className="mx-8 hidden max-w-md flex-1 lg:block">
          <div className="group relative">
            <AppIcon
              name="search"
              className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 transition-colors group-focus-within:text-primary"
            />
            <input
              className="w-full rounded-full border-none bg-slate-100 py-2 pl-10 pr-4 text-sm focus:ring-2 focus:ring-primary/50"
              value={searchValue}
              onChange={(event) => setSearchValue(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  submitSearch();
                }
              }}
              placeholder="搜索萌萌的内容..."
              type="text"
            />
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Link
            href="/upload"
            className="flex h-10 w-10 items-center justify-center rounded-full bg-slate-100 text-slate-600 transition-all hover:bg-primary/10 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2"
            title="上传"
          >
            <AppIcon name="upload" />
          </Link>

          <div className="mx-1 hidden h-6 w-[1px] bg-slate-200 sm:block" />

          {user ? (
            <>
              <Link href="/me" className="hidden items-center gap-2 rounded-full bg-primary/10 px-3 py-1 sm:flex">
                <div className="h-7 w-7 overflow-hidden rounded-full bg-white">
                  {user.avatar_url ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={user.avatar_url} alt={user.username} className="h-full w-full object-cover" />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-xs font-bold text-primary">
                      {user.username.slice(0, 1).toUpperCase()}
                    </div>
                  )}
                </div>
                <span className="max-w-[120px] truncate text-sm font-medium text-slate-700">{user.username}</span>
              </Link>
              <button
                type="button"
                onClick={onLogout}
                disabled={logoutPending}
                className="rounded-xl border border-slate-200 px-4 py-2 text-sm font-bold text-slate-700 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
              >
                {logoutPending ? "退出中..." : "退出"}
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={() => openAuthDialog("login")}
                className="rounded-full px-5 py-2 text-sm font-bold text-primary transition-all hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2"
              >
                登录
              </button>
              <button
                type="button"
                onClick={() => openAuthDialog("register")}
                className="rounded-full bg-primary px-5 py-2 text-sm font-bold text-white shadow-lg shadow-primary/25 transition-all hover:scale-105 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/70 focus-visible:ring-offset-2"
              >
                注册
              </button>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
