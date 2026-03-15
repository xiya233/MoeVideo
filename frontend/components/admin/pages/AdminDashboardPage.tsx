"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useAuth } from "@/components/auth/AuthProvider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { adminApi } from "@/lib/admin/api";

function MetricCard({ title, value }: { title: string; value: number }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm text-slate-500">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-semibold">{value}</div>
      </CardContent>
    </Card>
  );
}

export function AdminDashboardPage() {
  const { request } = useAuth();
  const queryClient = useQueryClient();
  const overviewQuery = useQuery({
    queryKey: ["admin-overview"],
    queryFn: () => adminApi.getOverview(request),
  });
  const clearImportsMutation = useMutation({
    mutationFn: () => adminApi.clearAllFinishedImportJobs(request),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    },
  });

  if (overviewQuery.isLoading) {
    return <div className="text-sm text-slate-500">正在加载仪表盘...</div>;
  }

  if (overviewQuery.isError || !overviewQuery.data) {
    const message = overviewQuery.error instanceof Error ? overviewQuery.error.message : "加载失败";
    return <div className="text-sm text-rose-600">仪表盘加载失败：{message}</div>;
  }

  const { metrics, recent_failed_jobs: failedJobs, recent_actions: recentActions } = overviewQuery.data;

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold">Dashboard</h2>
        <p className="text-sm text-slate-500">平台核心指标与最近后台操作概览</p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard title="视频总数" value={metrics.videos_total} />
        <MetricCard title="转码中视频" value={metrics.videos_processing} />
        <MetricCard title="转码失败任务" value={metrics.transcode_failed} />
        <MetricCard title="活跃用户" value={metrics.users_active} />
        <MetricCard title="用户总数" value={metrics.users_total} />
        <MetricCard title="今日上传" value={metrics.uploads_today} />
        <MetricCard title="今日新增用户" value={metrics.users_today} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>导入记录维护</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <p className="text-sm text-slate-500">清理所有用户已结束导入记录（succeeded / partial / failed）。</p>
            <Button
              variant="destructive"
              size="sm"
              onClick={() => void clearImportsMutation.mutateAsync()}
              disabled={clearImportsMutation.isPending}
            >
              {clearImportsMutation.isPending ? "清理中..." : "清理导入记录"}
            </Button>
            {clearImportsMutation.data ? (
              <p className="text-xs text-slate-500">已清理 {clearImportsMutation.data.deleted} 条导入记录</p>
            ) : null}
            {clearImportsMutation.isError ? (
              <p className="text-xs text-rose-600">
                {clearImportsMutation.error instanceof Error ? clearImportsMutation.error.message : "清理导入记录失败"}
              </p>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>最近失败转码</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {failedJobs.length === 0 ? <p className="text-sm text-slate-500">暂无失败任务</p> : null}
            {failedJobs.map((job) => (
              <div key={job.id} className="rounded-md border border-slate-200 p-3 text-sm">
                <p className="font-medium">job: {job.id}</p>
                <p className="text-slate-500">video: {job.video_id}</p>
                <p className="line-clamp-2 text-rose-600">{job.last_error || "未知错误"}</p>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>最近后台动作</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {recentActions.length === 0 ? <p className="text-sm text-slate-500">暂无动作记录</p> : null}
            {recentActions.map((item) => (
              <div key={item.id} className="rounded-md border border-slate-200 p-3 text-sm">
                <p className="font-medium">{item.action}</p>
                <p className="text-slate-500">
                  {item.actor.username} · {item.resource_type}/{item.resource_id}
                </p>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
