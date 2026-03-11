"use client";

import { useMemo, useState, type FormEvent } from "react";

import { ApiError } from "@/lib/api/types";
import { AppIcon } from "@/components/common/AppIcon";
import { useAuth } from "@/components/auth/AuthProvider";
import { cn } from "@/lib/utils/cn";

function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败，请稍后重试";
}

export function AuthDialog() {
  const {
    isAuthDialogOpen,
    authDialogMode,
    closeAuthDialog,
    login,
    register,
    openAuthDialog,
  } = useAuth();

  const [isSubmitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const [loginAccount, setLoginAccount] = useState("");
  const [loginPassword, setLoginPassword] = useState("");

  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  const title = useMemo(() => (authDialogMode === "login" ? "欢迎回来" : "创建账号"), [authDialogMode]);

  if (!isAuthDialogOpen) {
    return null;
  }

  const onClose = () => {
    if (isSubmitting) {
      return;
    }
    setError("");
    closeAuthDialog();
  };

  const onSubmitLogin = async (event: FormEvent) => {
    event.preventDefault();
    if (!loginAccount.trim() || !loginPassword) {
      setError("请输入账号和密码");
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      await login({ account: loginAccount.trim(), password: loginPassword });
    } catch (err) {
      setError(extractErrorMessage(err));
    } finally {
      setSubmitting(false);
    }
  };

  const onSubmitRegister = async (event: FormEvent) => {
    event.preventDefault();
    if (!username.trim() || !email.trim() || !password || !confirmPassword) {
      setError("请填写完整信息");
      return;
    }
    if (password.length < 8) {
      setError("密码至少 8 位");
      return;
    }
    if (password !== confirmPassword) {
      setError("两次输入的密码不一致");
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      await register({
        username: username.trim(),
        email: email.trim(),
        password,
        password_confirm: confirmPassword,
      });
    } catch (err) {
      setError(extractErrorMessage(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/45 px-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-xl border border-primary/10 bg-white p-6 shadow-2xl"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-xl font-bold text-slate-900">{title}</h3>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-2 text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60"
          >
            <AppIcon name="close" size={18} />
          </button>
        </div>

        <div className="mb-5 flex rounded-xl bg-primary/10 p-1">
          <button
            type="button"
            className={cn(
              "flex-1 rounded-lg px-3 py-2 text-sm font-semibold transition-all",
              authDialogMode === "login" ? "bg-white text-slate-900 shadow-sm" : "text-slate-500",
            )}
            onClick={() => openAuthDialog("login")}
          >
            登录
          </button>
          <button
            type="button"
            className={cn(
              "flex-1 rounded-lg px-3 py-2 text-sm font-semibold transition-all",
              authDialogMode === "register" ? "bg-white text-slate-900 shadow-sm" : "text-slate-500",
            )}
            onClick={() => openAuthDialog("register")}
          >
            注册
          </button>
        </div>

        {authDialogMode === "login" ? (
          <form className="space-y-3" onSubmit={onSubmitLogin}>
            <label className="block">
              <span className="mb-1 block text-xs font-semibold text-slate-600">用户名或邮箱</span>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/60"
                placeholder="输入用户名或邮箱"
                value={loginAccount}
                onChange={(event) => setLoginAccount(event.target.value)}
                autoComplete="username"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-semibold text-slate-600">密码</span>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/60"
                placeholder="输入密码"
                value={loginPassword}
                onChange={(event) => setLoginPassword(event.target.value)}
                type="password"
                autoComplete="current-password"
              />
            </label>
            {error ? <p className="text-xs font-medium text-rose-500">{error}</p> : null}
            <button
              type="submit"
              disabled={isSubmitting}
              className="mt-1 w-full rounded-xl bg-primary px-4 py-2.5 text-sm font-bold text-white transition-all hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {isSubmitting ? "登录中..." : "登录"}
            </button>
          </form>
        ) : (
          <form className="space-y-3" onSubmit={onSubmitRegister}>
            <label className="block">
              <span className="mb-1 block text-xs font-semibold text-slate-600">用户名</span>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/60"
                placeholder="输入用户名"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                autoComplete="username"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-semibold text-slate-600">邮箱</span>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/60"
                placeholder="输入邮箱"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                type="email"
                autoComplete="email"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-semibold text-slate-600">密码</span>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/60"
                placeholder="至少 8 位"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                type="password"
                autoComplete="new-password"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-semibold text-slate-600">确认密码</span>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/60"
                placeholder="再次输入密码"
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
                type="password"
                autoComplete="new-password"
              />
            </label>
            {error ? <p className="text-xs font-medium text-rose-500">{error}</p> : null}
            <button
              type="submit"
              disabled={isSubmitting}
              className="mt-1 w-full rounded-xl bg-primary px-4 py-2.5 text-sm font-bold text-white transition-all hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {isSubmitting ? "注册中..." : "注册并登录"}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
