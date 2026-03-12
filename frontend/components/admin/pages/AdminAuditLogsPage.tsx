"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { adminApi } from "@/lib/admin/api";

export function AdminAuditLogsPage() {
  const { request } = useAuth();

  const [action, setAction] = useState("");
  const [resourceType, setResourceType] = useState("");
  const [actorID, setActorID] = useState("");
  const [cursor, setCursor] = useState<string | undefined>();

  const auditQuery = useQuery({
    queryKey: ["admin-audit-logs", action, resourceType, actorID, cursor],
    queryFn: () =>
      adminApi.listAuditLogs(request, {
        action: action || undefined,
        resource_type: resourceType || undefined,
        actor_id: actorID || undefined,
        cursor,
        limit: 20,
      }),
  });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-semibold">Audit Logs</h2>
        <p className="text-sm text-slate-500">后台写操作审计记录检索。</p>
      </div>

      <div className="grid grid-cols-1 gap-3 rounded-xl border border-slate-200 bg-white p-4 md:grid-cols-4">
        <Input value={actorID} onChange={(e) => setActorID(e.target.value)} placeholder="actor_id" />

        <Input value={action} onChange={(e) => setAction(e.target.value)} placeholder="action，如 user.patch" />

        <Select
          value={resourceType || "all"}
          onValueChange={(value) => setResourceType(value === "all" ? "" : value)}
        >
          <SelectTrigger>
            <SelectValue placeholder="resource_type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部资源</SelectItem>
            <SelectItem value="video">video</SelectItem>
            <SelectItem value="comment">comment</SelectItem>
            <SelectItem value="user">user</SelectItem>
            <SelectItem value="transcode_job">transcode_job</SelectItem>
          </SelectContent>
        </Select>

        <div className="flex gap-2">
          <Button onClick={() => setCursor(undefined)}>刷新</Button>
          <Button
            variant="outline"
            onClick={() => {
              setAction("");
              setResourceType("");
              setActorID("");
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
            <TableHead>时间</TableHead>
            <TableHead>操作人</TableHead>
            <TableHead>动作</TableHead>
            <TableHead>资源</TableHead>
            <TableHead>IP</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(auditQuery.data?.items ?? []).map((log) => (
            <TableRow key={log.id}>
              <TableCell className="text-xs text-slate-500">{log.created_at}</TableCell>
              <TableCell>{log.actor.username}</TableCell>
              <TableCell className="font-medium">{log.action}</TableCell>
              <TableCell className="text-xs text-slate-500">
                {log.resource_type}/{log.resource_id}
              </TableCell>
              <TableCell className="text-xs text-slate-500">{log.ip || "-"}</TableCell>
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
          disabled={!auditQuery.data?.next_cursor}
          onClick={() => setCursor(auditQuery.data?.next_cursor)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
