"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

import { AdminShell } from "@/components/admin/AdminShell";
import { EmptyState } from "@/components/common/EmptyState";
import { useAuth } from "@/components/auth/AuthProvider";

export function AdminGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { ready, session, user } = useAuth();

  useEffect(() => {
    if (!ready) {
      return;
    }
    if (!session) {
      router.replace("/admin/login");
    }
  }, [ready, router, session]);

  if (!ready || !session) {
    return <div className="p-8 text-sm text-slate-500">正在验证登录状态...</div>;
  }

  if (!user?.role) {
    return <div className="p-8 text-sm text-slate-500">正在验证管理员权限...</div>;
  }

  if (user?.role !== "admin") {
    return (
      <div className="p-8">
        <EmptyState title="403 无权限" description="当前账号不是管理员，无法访问后台管理。" />
      </div>
    );
  }

  return <AdminShell>{children}</AdminShell>;
}
