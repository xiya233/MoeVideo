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

  return (
    <header className="sticky top-0 z-50 border-b border-sky-200/40 bg-[rgba(245,247,248,0.82)] px-4 py-3 backdrop-blur-md md:px-10">
      <div className="mx-auto flex w-full max-w-[1400px] items-center justify-between gap-4">
        <div className="flex items-center gap-8">
          <Link className="flex items-center gap-2 text-sky-500" href="/">
            <span className="material-symbols-outlined text-4xl" style={{ fontVariationSettings: "'FILL' 1" }}>
              face_5
            </span>
            <h2 className="text-xl font-bold tracking-tight text-slate-900">MoeVideo</h2>
          </Link>
          <nav className="hidden items-center gap-6 md:flex">
            <Link
              href="/"
              className={cn(
                "text-sm font-semibold transition-colors",
                pathname === "/" ? "text-sky-600" : "text-slate-700 hover:text-sky-500",
              )}
            >
              首页
            </Link>
            <Link
              href="/#categories"
              className="text-sm font-semibold text-slate-700 transition-colors hover:text-sky-500"
            >
              分类
            </Link>
            {user ? (
              <Link
                href="/upload"
                className={cn(
                  "text-sm font-semibold transition-colors",
                  pathname === "/upload" ? "text-sky-600" : "text-slate-700 hover:text-sky-500",
                )}
              >
                上传中心
              </Link>
            ) : null}
          </nav>
        </div>

        <div className="hidden w-full max-w-md items-center rounded-full bg-white/85 px-3 py-2 shadow-sm ring-1 ring-sky-100 lg:flex">
          <span className="material-symbols-outlined text-lg text-slate-400">search</span>
          <input
            className="ml-2 w-full bg-transparent text-sm text-slate-800 outline-none placeholder:text-slate-400"
            value={searchValue}
            onChange={(event) => setSearchValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                submitSearch();
              }
            }}
            placeholder="搜索萌萌的内容..."
          />
          <button
            type="button"
            onClick={submitSearch}
            className="rounded-full bg-sky-500 px-3 py-1 text-xs font-bold text-white transition hover:bg-sky-600"
          >
            搜索
          </button>
        </div>

        <div className="flex items-center gap-2">
          <Link
            href="/upload"
            className="flex h-10 w-10 items-center justify-center rounded-full bg-white text-slate-600 ring-1 ring-slate-200 transition hover:bg-sky-50 hover:text-sky-600"
            title="上传"
          >
            <span className="material-symbols-outlined">upload</span>
          </Link>

          {user ? (
            <>
              <div className="hidden items-center gap-2 rounded-full bg-white px-3 py-1 ring-1 ring-slate-200 sm:flex">
                <div className="h-7 w-7 overflow-hidden rounded-full bg-sky-100">
                  {user.avatar_url ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={user.avatar_url} alt={user.username} className="h-full w-full object-cover" />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-xs font-bold text-sky-700">
                      {user.username.slice(0, 1).toUpperCase()}
                    </div>
                  )}
                </div>
                <span className="max-w-[120px] truncate text-sm font-semibold text-slate-700">{user.username}</span>
              </div>
              <button
                type="button"
                onClick={onLogout}
                disabled={logoutPending}
                className="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-semibold text-slate-700 transition hover:border-sky-400 hover:text-sky-600 disabled:cursor-not-allowed disabled:opacity-70"
              >
                {logoutPending ? "退出中..." : "退出"}
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={() => openAuthDialog("login")}
                className="rounded-full px-5 py-2 text-sm font-bold text-sky-600 transition hover:bg-sky-50"
              >
                登录
              </button>
              <button
                type="button"
                onClick={() => openAuthDialog("register")}
                className="rounded-full bg-sky-500 px-5 py-2 text-sm font-bold text-white shadow-lg shadow-sky-400/30 transition hover:scale-[1.03] hover:bg-sky-600"
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
