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

export function AdminCommentsPage() {
  const { request } = useAuth();
  const queryClient = useQueryClient();

  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const [cursor, setCursor] = useState<string | undefined>();

  const commentsQuery = useQuery({
    queryKey: ["admin-comments", q, status, cursor],
    queryFn: () =>
      adminApi.listComments(request, {
        q: q || undefined,
        status: status || undefined,
        cursor,
        limit: 20,
      }),
  });

  const actionMutation = useMutation({
    mutationFn: ({ action, commentIDs }: { action: "delete" | "restore"; commentIDs: string[] }) =>
      adminApi.commentsAction(request, action, commentIDs),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-comments"] });
    },
  });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-semibold">Comments</h2>
        <p className="text-sm text-slate-500">按关键词筛选评论，批量删除/恢复。</p>
      </div>

      <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-200 bg-white p-4 md:grid-cols-3">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="按评论内容搜索" />

        <Select value={status || "all"} onValueChange={(value) => setStatus(value === "all" ? "" : value)}>
          <SelectTrigger>
            <SelectValue placeholder="状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="active">active</SelectItem>
            <SelectItem value="deleted">deleted</SelectItem>
          </SelectContent>
        </Select>

        <div className="flex gap-2">
          <Button onClick={() => setCursor(undefined)}>刷新</Button>
          <Button
            variant="outline"
            onClick={() => {
              setQ("");
              setStatus("");
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
            <TableHead>评论</TableHead>
            <TableHead>作者</TableHead>
            <TableHead>所属视频</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(commentsQuery.data?.items ?? []).map((comment) => (
            <TableRow key={comment.id}>
              <TableCell>
                <p className="max-w-[460px] truncate">{comment.content}</p>
                <p className="text-xs text-slate-500">{comment.id}</p>
              </TableCell>
              <TableCell>{comment.username}</TableCell>
              <TableCell>
                <p className="max-w-[220px] truncate text-sm">{comment.video_title || comment.video_id}</p>
              </TableCell>
              <TableCell>
                <Badge variant={comment.status === "active" ? "success" : "secondary"}>{comment.status}</Badge>
              </TableCell>
              <TableCell>
                <div className="flex justify-end gap-2">
                  {comment.status === "active" ? (
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={() => void actionMutation.mutateAsync({ action: "delete", commentIDs: [comment.id] })}
                    >
                      删除
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void actionMutation.mutateAsync({ action: "restore", commentIDs: [comment.id] })}
                    >
                      恢复
                    </Button>
                  )}
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
          disabled={!commentsQuery.data?.next_cursor}
          onClick={() => setCursor(commentsQuery.data?.next_cursor)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
