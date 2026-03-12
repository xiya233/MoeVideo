"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

export function AdminLoginPage() {
  const router = useRouter();
  const { ready, session, user, login, logout, request } = useAuth();

  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  useEffect(() => {
    if (!ready) {
      return;
    }
    if (session && user?.role === "admin") {
      router.replace("/admin");
    }
  }, [ready, router, session, user?.role]);

  const onSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!account.trim() || !password) {
      setError("请输入账号和密码");
      return;
    }

    setPending(true);
    setError("");
    try {
      await login({ account: account.trim(), password });
      const me = await request<{ role?: string }>("/users/me", { auth: true });
      if (me.role !== "admin") {
        await logout();
        setError("该账号不是管理员，无法登录后台");
        return;
      }
      router.replace("/admin");
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>管理员登录</CardTitle>
          <CardDescription>使用管理员账号登录 MoeVideo 后台</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={onSubmit}>
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">用户名或邮箱</label>
              <Input value={account} onChange={(e) => setAccount(e.target.value)} placeholder="admin@example.com" />
            </div>
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">密码</label>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="输入管理员密码"
              />
            </div>
            {error ? <p className="text-xs text-rose-600">{error}</p> : null}
            <Button className="w-full" disabled={pending} type="submit">
              {pending ? "登录中..." : "登录后台"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
