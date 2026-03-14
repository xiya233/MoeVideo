"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { adminApi } from "@/lib/admin/api";

const BANNER_SIZE = 5;
const UNSELECTED = "__unselected__";

export function AdminBannersPage() {
  const { request } = useAuth();
  const queryClient = useQueryClient();

  const [videoIDs, setVideoIDs] = useState<string[]>(Array.from({ length: BANNER_SIZE }, () => ""));
  const [saveError, setSaveError] = useState("");
  const [saveMessage, setSaveMessage] = useState("");

  const bannersQuery = useQuery({
    queryKey: ["admin-featured-banners"],
    queryFn: () => adminApi.getFeaturedBanners(request),
  });

  const candidateVideosQuery = useQuery({
    queryKey: ["admin-banner-candidates"],
    queryFn: () =>
      adminApi.listVideos(request, {
        status: "published",
        visibility: "public",
        limit: 100,
      }),
  });

  useEffect(() => {
    if (!bannersQuery.data) {
      return;
    }
    const next = Array.from({ length: BANNER_SIZE }, (_, idx) => bannersQuery.data?.video_ids?.[idx] ?? "");
    setVideoIDs(next);
  }, [bannersQuery.data]);

  const candidateList = useMemo(() => {
    const map = new Map<string, { id: string; title: string }>();
    for (const item of candidateVideosQuery.data?.items ?? []) {
      map.set(item.id, {
        id: item.id,
        title: item.title,
      });
    }
    for (const item of bannersQuery.data?.items ?? []) {
      if (!item?.video?.id) {
        continue;
      }
      if (!map.has(item.video.id)) {
        map.set(item.video.id, {
          id: item.video.id,
          title: item.video.title,
        });
      }
    }
    return Array.from(map.values());
  }, [bannersQuery.data?.items, candidateVideosQuery.data?.items]);

  const saveMutation = useMutation({
    mutationFn: (nextVideoIDs: string[]) => adminApi.setFeaturedBanners(request, nextVideoIDs),
    onSuccess: async () => {
      setSaveMessage("精选海报已保存");
      setSaveError("");
      await queryClient.invalidateQueries({ queryKey: ["admin-featured-banners"] });
      await queryClient.invalidateQueries({ queryKey: ["site-settings-public"] });
    },
    onError: (error) => {
      setSaveError(error instanceof Error ? error.message : "保存失败");
      setSaveMessage("");
    },
  });

  const updateSlot = (index: number, value: string) => {
    setVideoIDs((prev) => {
      const next = [...prev];
      next[index] = value === UNSELECTED ? "" : value;
      return next;
    });
  };

  const onSave = async () => {
    setSaveError("");
    setSaveMessage("");
    const normalized = videoIDs.map((id) => id.trim());
    if (normalized.some((id) => !id)) {
      setSaveError("请为 5 个海报位次都选择视频");
      return;
    }
    const seen = new Set(normalized);
    if (seen.size !== normalized.length) {
      setSaveError("精选海报不允许重复视频");
      return;
    }
    await saveMutation.mutateAsync(normalized);
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold">Banner Management</h2>
        <p className="text-sm text-slate-500">配置首页轮播海报（固定 5 条，按顺序展示）。</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>首页精选海报</CardTitle>
          <CardDescription>仅可选择 `published + public` 视频。顺序即首页轮播顺序。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {bannersQuery.isLoading || candidateVideosQuery.isLoading ? (
            <p className="text-sm text-slate-500">加载数据中...</p>
          ) : null}

          {Array.from({ length: BANNER_SIZE }).map((_, idx) => (
            <div key={idx} className="space-y-1">
              <label className="block text-xs font-semibold text-slate-500">海报位次 #{idx + 1}</label>
              <Select value={videoIDs[idx] || UNSELECTED} onValueChange={(value) => updateSlot(idx, value)}>
                <SelectTrigger>
                  <SelectValue placeholder="请选择视频" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={UNSELECTED}>请选择视频</SelectItem>
                  {candidateList.map((video) => (
                    <SelectItem key={video.id} value={video.id}>
                      {video.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ))}

          {saveError ? <p className="text-xs text-rose-600">{saveError}</p> : null}
          {saveMessage ? <p className="text-xs text-emerald-600">{saveMessage}</p> : null}

          <div className="flex justify-end">
            <Button onClick={() => void onSave()} disabled={saveMutation.isPending || bannersQuery.isLoading || candidateVideosQuery.isLoading}>
              {saveMutation.isPending ? "保存中..." : "保存精选海报"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

