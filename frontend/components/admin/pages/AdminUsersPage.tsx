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

export function AdminUsersPage() {
  const { request } = useAuth();
  const queryClient = useQueryClient();

  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const [role, setRole] = useState("");
  const [cursor, setCursor] = useState<string | undefined>();

  const usersQuery = useQuery({
    queryKey: ["admin-users", q, status, role, cursor],
    queryFn: () =>
      adminApi.listUsers(request, {
        q: q || undefined,
        status: status || undefined,
        role: role || undefined,
        cursor,
        limit: 20,
      }),
  });

  const patchMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: { status?: "active" | "disabled"; role?: "user" | "admin" } }) =>
      adminApi.patchUser(request, id, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-users"] });
    },
  });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-semibold">Users</h2>
        <p className="text-sm text-slate-500">用户检索、封禁解封与管理员授权。</p>
      </div>

      <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-200 bg-white p-4 md:grid-cols-4">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="按用户名或邮箱搜索" />

        <Select value={status || "all"} onValueChange={(value) => setStatus(value === "all" ? "" : value)}>
          <SelectTrigger>
            <SelectValue placeholder="状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="active">active</SelectItem>
            <SelectItem value="disabled">disabled</SelectItem>
          </SelectContent>
        </Select>

        <Select value={role || "all"} onValueChange={(value) => setRole(value === "all" ? "" : value)}>
          <SelectTrigger>
            <SelectValue placeholder="角色" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部角色</SelectItem>
            <SelectItem value="user">user</SelectItem>
            <SelectItem value="admin">admin</SelectItem>
          </SelectContent>
        </Select>

        <div className="flex gap-2">
          <Button onClick={() => setCursor(undefined)}>刷新</Button>
          <Button
            variant="outline"
            onClick={() => {
              setQ("");
              setStatus("");
              setRole("");
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
            <TableHead>用户</TableHead>
            <TableHead>角色</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>统计</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(usersQuery.data?.items ?? []).map((user) => (
            <TableRow key={user.id}>
              <TableCell>
                <p className="font-medium">{user.username}</p>
                <p className="text-xs text-slate-500">{user.email}</p>
              </TableCell>
              <TableCell>
                <Badge variant={user.role === "admin" ? "warning" : "secondary"}>{user.role}</Badge>
              </TableCell>
              <TableCell>
                <Badge variant={user.status === "active" ? "success" : "destructive"}>{user.status}</Badge>
              </TableCell>
              <TableCell className="text-xs text-slate-500">
                {user.videos_count} 视频 / {user.followers_count} 粉丝
              </TableCell>
              <TableCell>
                <div className="flex justify-end gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      void patchMutation.mutateAsync({
                        id: user.id,
                        payload: { status: user.status === "active" ? "disabled" : "active" },
                      })
                    }
                  >
                    {user.status === "active" ? "封禁" : "解封"}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      void patchMutation.mutateAsync({
                        id: user.id,
                        payload: { role: user.role === "admin" ? "user" : "admin" },
                      })
                    }
                  >
                    {user.role === "admin" ? "撤销管理员" : "设为管理员"}
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
          disabled={!usersQuery.data?.next_cursor}
          onClick={() => setCursor(usersQuery.data?.next_cursor)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
