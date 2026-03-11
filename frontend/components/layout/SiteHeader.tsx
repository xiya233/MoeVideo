"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
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

  const isUpload = pathname === "/upload";
  const isVideo = pathname.startsWith("/videos/");
  const isHome = pathname === "/";

  if (isUpload) {
    return (
      <header className="sticky top-0 z-50 border-b border-primary/10 bg-white/80 px-6 py-3 backdrop-blur-md md:px-10">
        <div className="mx-auto flex w-full max-w-[1400px] items-center justify-between whitespace-nowrap">
          <div className="flex items-center gap-3 text-primary">
            <div className="flex size-8 items-center justify-center rounded-lg bg-primary/10">
              <span className="material-symbols-outlined font-bold text-primary">movie_filter</span>
            </div>
            <h2 className="text-xl font-bold leading-tight tracking-tight text-slate-900">喵影上传</h2>
          </div>

          <div className="flex flex-1 items-center justify-end gap-4 md:gap-8">
            <nav className="hidden items-center gap-8 md:flex">
              <Link
                href="/"
                className="text-sm font-medium text-slate-600 transition-colors hover:text-primary"
              >
                首页
              </Link>
              <Link
                href="/"
                className="text-sm font-medium text-slate-600 transition-colors hover:text-primary"
              >
                我的视频
              </Link>
              <span className="text-sm font-bold text-primary">上传中心</span>
            </nav>

            {user ? (
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  className="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary transition-all hover:bg-primary hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2"
                >
                  <span className="material-symbols-outlined text-[20px]">notifications</span>
                </button>
                <button
                  type="button"
                  className="hidden size-10 items-center justify-center rounded-full bg-primary/10 text-primary transition-all hover:bg-primary hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2 sm:flex"
                >
                  <span className="material-symbols-outlined text-[20px]">account_circle</span>
                </button>
                <div className="h-10 w-10 overflow-hidden rounded-full border-2 border-primary/30 bg-primary/20">
                  {user.avatar_url ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={user.avatar_url} alt={user.username} className="h-full w-full object-cover" />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-xs font-bold text-primary">
                      {user.username.slice(0, 1).toUpperCase()}
                    </div>
                  )}
                </div>
                <button
                  type="button"
                  onClick={onLogout}
                  disabled={logoutPending}
                  className="rounded-full border border-slate-200 px-4 py-2 text-sm font-medium text-slate-600 transition-colors hover:border-primary/30 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {logoutPending ? "退出中..." : "退出"}
                </button>
              </div>
            ) : (
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => openAuthDialog("login")}
                  className="rounded-full px-4 py-2 text-sm font-bold text-primary transition-colors hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2"
                >
                  登录
                </button>
                <button
                  type="button"
                  onClick={() => openAuthDialog("register")}
                  className="rounded-full bg-primary px-4 py-2 text-sm font-bold text-white shadow-lg shadow-primary/30 transition-all hover:shadow-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/70 focus-visible:ring-offset-2"
                >
                  注册
                </button>
              </div>
            )}
          </div>
        </div>
      </header>
    );
  }

  return (
    <header className="sticky top-0 z-50 border-b border-primary/10 bg-white/80 px-4 py-3 backdrop-blur-md md:px-10">
      <div
        className={cn(
          "mx-auto flex w-full items-center justify-between gap-4",
          isHome ? "max-w-[1400px]" : "max-w-[1440px]",
        )}
      >
        <div className="flex items-center gap-8">
          <Link className="flex items-center gap-2 text-primary" href="/">
            {isHome ? (
              <span className="material-symbols-outlined text-4xl" style={{ fontVariationSettings: "'FILL' 1" }}>
                face_5
              </span>
            ) : (
              <span className="material-symbols-outlined text-3xl">play_circle</span>
            )}
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
              {isHome ? <span className="material-symbols-outlined text-xl">home</span> : null}
              首页
            </Link>
            <Link
              href="/#categories"
              className="flex items-center gap-1 text-sm font-medium text-slate-700 transition-colors hover:text-primary"
            >
              {isHome ? <span className="material-symbols-outlined text-xl">grid_view</span> : null}
              分类
            </Link>
            {user ? (
              <Link
                href="/upload"
                className={cn(
                  "text-sm font-medium transition-colors hover:text-primary",
                  pathname === "/upload" ? "text-primary" : "text-slate-700",
                )}
              >
                上传中心
              </Link>
            ) : null}
          </nav>
        </div>

        {isHome ? (
          <div className="mx-8 hidden max-w-md flex-1 lg:block">
            <div className="group relative">
              <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 transition-colors group-focus-within:text-primary">
                search
              </span>
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
        ) : (
          <div className="hidden w-full max-w-md flex-1 items-center rounded-xl border border-transparent bg-primary/10 px-3 py-1.5 transition-all focus-within:border-primary/30 sm:flex">
            <span className="material-symbols-outlined text-xl text-primary/60">search</span>
            <input
              className="w-full border-none bg-transparent text-sm placeholder:text-primary/40 focus:ring-0"
              value={searchValue}
              onChange={(event) => setSearchValue(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  submitSearch();
                }
              }}
              placeholder="搜索你感兴趣的视频..."
              type="text"
            />
          </div>
        )}

        <div className="flex items-center gap-2">
          <Link
            href="/upload"
            className="flex h-10 w-10 items-center justify-center rounded-full bg-slate-100 text-slate-600 transition-all hover:bg-primary/10 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2"
            title="上传"
          >
            <span className="material-symbols-outlined">upload</span>
          </Link>

          {isHome ? <div className="mx-1 hidden h-6 w-[1px] bg-slate-200 sm:block" /> : null}

          {user ? (
            <>
              <div className="hidden items-center gap-2 rounded-full bg-primary/10 px-3 py-1 sm:flex">
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
              </div>
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
                className={cn(
                  "rounded-full text-sm font-bold text-primary transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2",
                  isHome ? "px-5 py-2 hover:bg-primary/5" : "rounded-xl bg-primary/10 px-4 py-2 hover:bg-primary/20",
                )}
              >
                登录
              </button>
              <button
                type="button"
                onClick={() => openAuthDialog("register")}
                className={cn(
                  "text-sm font-bold text-white transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/70 focus-visible:ring-offset-2",
                  isHome
                    ? "rounded-full bg-primary px-5 py-2 shadow-lg shadow-primary/25 hover:scale-105 active:scale-95"
                    : "rounded-xl bg-primary px-4 py-2 hover:shadow-lg hover:shadow-primary/30",
                )}
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
