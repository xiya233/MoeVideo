"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { adminApi } from "@/lib/admin/api";

const statusVariant: Record<string, "outline" | "warning" | "success" | "destructive"> = {
  queued: "outline",
  processing: "warning",
  succeeded: "success",
  failed: "destructive",
};

export function AdminTranscodePage() {
  const { request } = useAuth();
  const queryClient = useQueryClient();

  const [status, setStatus] = useState("");
  const [videoID, setVideoID] = useState("");
  const [cursor, setCursor] = useState<string | undefined>();

  const jobsQuery = useQuery({
    queryKey: ["admin-transcode-jobs", status, videoID, cursor],
    queryFn: () =>
      adminApi.listTranscodeJobs(request, {
        status: status || undefined,
        video_id: videoID || undefined,
        cursor,
        limit: 20,
      }),
  });

  const retryMutation = useMutation({
    mutationFn: (jobId: string) => adminApi.retryTranscodeJob(request, jobId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-transcode-jobs"] });
    },
  });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-semibold">Transcode Jobs</h2>
        <p className="text-sm text-slate-500">查看任务状态与错误，手动重试失败任务。</p>
      </div>

      <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-200 bg-white p-4 md:grid-cols-3">
        <Select value={status || "all"} onValueChange={(value) => setStatus(value === "all" ? "" : value)}>
          <SelectTrigger>
            <SelectValue placeholder="任务状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="queued">queued</SelectItem>
            <SelectItem value="processing">processing</SelectItem>
            <SelectItem value="succeeded">succeeded</SelectItem>
            <SelectItem value="failed">failed</SelectItem>
          </SelectContent>
        </Select>

        <Input value={videoID} onChange={(e) => setVideoID(e.target.value)} placeholder="按 video_id 过滤" />

        <div className="flex gap-2">
          <Button onClick={() => setCursor(undefined)}>刷新</Button>
          <Button
            variant="outline"
            onClick={() => {
              setStatus("");
              setVideoID("");
              setCursor(undefined);
            }}
          >
            重置
          </Button>
        </div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>任务</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>尝试次数</TableHead>
            <TableHead>错误信息</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(jobsQuery.data?.items ?? []).map((job) => (
            <TableRow key={job.id}>
              <TableCell>
                <p className="font-medium">{job.id}</p>
                <p className="text-xs text-slate-500">video: {job.video_id}</p>
              </TableCell>
              <TableCell>
                <Badge variant={statusVariant[job.status] ?? "outline"}>{job.status}</Badge>
              </TableCell>
              <TableCell>
                {job.attempts}/{job.max_attempts}
              </TableCell>
              <TableCell className="max-w-[420px] truncate text-xs text-rose-600">{job.last_error || "-"}</TableCell>
              <TableCell>
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={retryMutation.isPending}
                    onClick={() => void retryMutation.mutateAsync(job.id)}
                  >
                    重试
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={() => setCursor(undefined)}>
          首页
        </Button>
        <Button
          variant="secondary"
          disabled={!jobsQuery.data?.next_cursor}
          onClick={() => setCursor(jobsQuery.data?.next_cursor)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
