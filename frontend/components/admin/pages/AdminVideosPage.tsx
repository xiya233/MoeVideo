"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { adminApi } from "@/lib/admin/api";

const statusBadge: Record<string, "default" | "secondary" | "success" | "warning" | "destructive" | "outline"> = {
  published: "success",
  processing: "warning",
  failed: "destructive",
  deleted: "secondary",
};

export function AdminVideosPage() {
  const { request } = useAuth();
  const queryClient = useQueryClient();

  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const [visibility, setVisibility] = useState("");
  const [cursor, setCursor] = useState<string | undefined>();
  const [selectedVideoId, setSelectedVideoId] = useState<string | null>(null);
  const [action, setAction] = useState<"publish" | "hide" | "soft_delete" | "restore" | "retry_transcode" | null>(null);

  const videosQuery = useQuery({
    queryKey: ["admin-videos", q, status, visibility, cursor],
    queryFn: () =>
      adminApi.listVideos(request, {
        q: q || undefined,
        status: status || undefined,
        visibility: visibility || undefined,
        cursor,
        limit: 20,
      }),
  });

  const actionMutation = useMutation({
    mutationFn: ({ id, act }: { id: string; act: string }) => adminApi.videoAction(request, id, act),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-videos"] });
    },
  });

  const selected = useMemo(
    () => videosQuery.data?.items.find((item) => item.id === selectedVideoId) ?? null,
    [videosQuery.data?.items, selectedVideoId],
  );

  const submitAction = async () => {
    if (!selectedVideoId || !action) {
      return;
    }
    await actionMutation.mutateAsync({ id: selectedVideoId, act: action });
    setAction(null);
    setSelectedVideoId(null);
  };

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-semibold">Videos</h2>
        <p className="text-sm text-slate-500">筛选视频并执行上下架、删除、恢复、重试转码等操作。</p>
      </div>

      <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-200 bg-white p-4 md:grid-cols-4">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="按标题搜索" />

        <Select value={status || "all"} onValueChange={(value) => setStatus(value === "all" ? "" : value)}>
          <SelectTrigger>
            <SelectValue placeholder="状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="processing">processing</SelectItem>
            <SelectItem value="published">published</SelectItem>
            <SelectItem value="failed">failed</SelectItem>
            <SelectItem value="deleted">deleted</SelectItem>
          </SelectContent>
        </Select>

        <Select value={visibility || "all"} onValueChange={(value) => setVisibility(value === "all" ? "" : value)}>
          <SelectTrigger>
            <SelectValue placeholder="可见性" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部可见性</SelectItem>
            <SelectItem value="public">public</SelectItem>
            <SelectItem value="private">private</SelectItem>
            <SelectItem value="unlisted">unlisted</SelectItem>
          </SelectContent>
        </Select>

        <div className="flex gap-2">
          <Button onClick={() => setCursor(undefined)}>刷新</Button>
          <Button
            variant="outline"
            onClick={() => {
              setQ("");
              setStatus("");
              setVisibility("");
              setCursor(undefined);
            }}
          >
            重置
          </Button>
        </div>
      </div>

      {videosQuery.isError ? (
        <p className="text-sm text-rose-600">加载失败：{videosQuery.error instanceof Error ? videosQuery.error.message : "unknown"}</p>
      ) : null}

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>标题</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>可见性</TableHead>
            <TableHead>上传者</TableHead>
            <TableHead>数据</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(videosQuery.data?.items ?? []).map((video) => (
            <TableRow key={video.id}>
              <TableCell>
                <p className="max-w-[380px] truncate font-medium">{video.title}</p>
                <p className="text-xs text-slate-500">{video.id}</p>
              </TableCell>
              <TableCell>
                <Badge variant={statusBadge[video.status] ?? "outline"}>{video.status}</Badge>
              </TableCell>
              <TableCell>{video.visibility}</TableCell>
              <TableCell>{video.uploader.username}</TableCell>
              <TableCell className="text-xs text-slate-500">
                {video.views_count} 播放 / {video.comments_count} 评论
              </TableCell>
              <TableCell>
                <div className="flex justify-end gap-2">
                  <Button size="sm" variant="outline" onClick={() => {
                    setSelectedVideoId(video.id);
                    setAction("publish");
                  }}>
                    发布
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => {
                    setSelectedVideoId(video.id);
                    setAction("hide");
                  }}>
                    隐藏
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => {
                    setSelectedVideoId(video.id);
                    setAction("retry_transcode");
                  }}>
                    重试转码
                  </Button>
                  <Button size="sm" variant="destructive" onClick={() => {
                    setSelectedVideoId(video.id);
                    setAction(video.status === "deleted" ? "restore" : "soft_delete");
                  }}>
                    {video.status === "deleted" ? "恢复" : "删除"}
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
          disabled={!videosQuery.data?.next_cursor}
          onClick={() => setCursor(videosQuery.data?.next_cursor)}
        >
          下一页
        </Button>
      </div>

      <Dialog open={!!action && !!selected} onOpenChange={(open) => (!open ? setAction(null) : null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认操作</DialogTitle>
            <DialogDescription>
              确认对视频 <strong>{selected?.title}</strong> 执行 <strong>{action}</strong> 吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAction(null)}>
              取消
            </Button>
            <Button onClick={() => void submitAction()} disabled={actionMutation.isPending}>
              {actionMutation.isPending ? "提交中..." : "确认"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
