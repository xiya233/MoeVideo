"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon } from "@/components/common/AppIcon";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils/cn";
import { usePublicSiteSettings } from "@/lib/site-settings/public";

type CaptchaData = {
  captcha_id: string;
  image_data: string;
  expires_at: string;
};

export function AdminLoginPage() {
  const router = useRouter();
  const { ready, session, user, login, logout, request } = useAuth();
  const siteSettingsQuery = usePublicSiteSettings();

  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [captchaID, setCaptchaID] = useState("");
  const [captchaCode, setCaptchaCode] = useState("");
  const [captchaImage, setCaptchaImage] = useState("");
  const [captchaLoading, setCaptchaLoading] = useState(false);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  const siteTitle = siteSettingsQuery.data?.site_title?.trim() || "MoeVideo";
  const siteDescription = siteSettingsQuery.data?.site_description?.trim() || "MoeVideo 后台管理";

  useEffect(() => {
    if (!ready) {
      return;
    }
    if (session && user?.role === "admin") {
      router.replace("/admin");
    }
  }, [ready, router, session, user?.role]);

  const loadCaptcha = async () => {
    setCaptchaLoading(true);
    try {
      const data = await request<CaptchaData>("/auth/captcha?scene=login", { auth: false });
      setCaptchaID(data.captcha_id);
      setCaptchaImage(data.image_data);
      setCaptchaCode("");
    } catch {
      setCaptchaID("");
      setCaptchaImage("");
    } finally {
      setCaptchaLoading(false);
    }
  };

  useEffect(() => {
    void loadCaptcha();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!account.trim() || !password) {
      setError("请输入账号和密码");
      return;
    }
    if (!captchaID || !captchaCode.trim()) {
      setError("请输入验证码");
      return;
    }

    setPending(true);
    setError("");
    try {
      await login({
        account: account.trim(),
        password,
        captcha_id: captchaID,
        captcha_code: captchaCode.trim(),
      });
      const me = await request<{ role?: string }>("/users/me", { auth: true });
      if (me.role !== "admin") {
        await logout();
        setError("该账号不是管理员，无法登录后台");
        await loadCaptcha();
        return;
      }
      router.replace("/admin");
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
      await loadCaptcha();
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>{siteTitle} 管理员登录</CardTitle>
          <CardDescription>{siteDescription}</CardDescription>
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
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">验证码</label>
              <div className="flex gap-2">
                <Input value={captchaCode} onChange={(e) => setCaptchaCode(e.target.value)} placeholder="输入验证码" />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-10 w-10 px-0"
                  disabled={captchaLoading}
                  onClick={() => void loadCaptcha()}
                >
                  <AppIcon name="autorenew" size={16} className={cn(captchaLoading && "animate-spin")} />
                </Button>
              </div>
              <div className="mt-2 h-12 overflow-hidden rounded-md border border-slate-200 bg-slate-50">
                {captchaImage ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={captchaImage} alt="验证码" className="h-full w-full object-cover" />
                ) : (
                  <div className="flex h-full items-center justify-center text-xs text-slate-500">验证码加载中...</div>
                )}
              </div>
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
