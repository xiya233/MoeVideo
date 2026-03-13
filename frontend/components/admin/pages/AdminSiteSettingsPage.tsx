"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { adminApi } from "@/lib/admin/api";
import type { AdminSiteCategory } from "@/lib/admin/types";
import type { UploadCompleteData, UploadTicket } from "@/lib/dto";
import { mapUploadCompleteData, mapUploadTicket } from "@/lib/dto/mappers";

const MAX_LOGO_SIZE = 10 * 1024 * 1024;
const ALLOWED_LOGO_TYPES = new Set(["image/jpeg", "image/png", "image/webp"]);

function headersToText(headers?: Record<string, string>): string {
  if (!headers) {
    return "";
  }
  return Object.entries(headers)
    .map(([key, value]) => `${key}: ${value}`)
    .join("\n");
}

function parseHeadersText(input: string): Record<string, string> {
  const map: Record<string, string> = {};
  const lines = input
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  for (const line of lines) {
    const sepIndex = line.indexOf(":");
    if (sepIndex <= 0) {
      throw new Error(`Header 格式错误: ${line}`);
    }
    const key = line.slice(0, sepIndex).trim();
    const value = line.slice(sepIndex + 1).trim();
    if (!key) {
      throw new Error("Header key 不能为空");
    }
    map[key] = value;
  }
  return map;
}

export function AdminSiteSettingsPage() {
  const { request, uploadBinary } = useAuth();
  const queryClient = useQueryClient();

  const [siteTitle, setSiteTitle] = useState("");
  const [siteDescription, setSiteDescription] = useState("");
  const [registerEnabled, setRegisterEnabled] = useState(true);
  const [siteLogoURL, setSiteLogoURL] = useState("");
  const [saveMessage, setSaveMessage] = useState("");
  const [saveError, setSaveError] = useState("");
  const [logoBusy, setLogoBusy] = useState(false);
  const [ytdlpParamMode, setYTDLPParamMode] = useState<"safe" | "advanced">("safe");
  const [ytdlpFormat, setYTDLPFormat] = useState("");
  const [ytdlpExtractorArgs, setYTDLPExtractorArgs] = useState("");
  const [ytdlpUserAgent, setYTDLPUserAgent] = useState("");
  const [ytdlpReferer, setYTDLPReferer] = useState("");
  const [ytdlpHeadersText, setYTDLPHeadersText] = useState("");
  const [ytdlpSocketTimeout, setYTDLPSocketTimeout] = useState("");
  const [ytdlpMetadataArgsRaw, setYTDLPMetadataArgsRaw] = useState("");
  const [ytdlpDownloadArgsRaw, setYTDLPDownloadArgsRaw] = useState("");

  const [createSlug, setCreateSlug] = useState("");
  const [createName, setCreateName] = useState("");
  const [createSortOrder, setCreateSortOrder] = useState("0");
  const [createActive, setCreateActive] = useState(true);
  const [categoryError, setCategoryError] = useState("");

  const [editingCategory, setEditingCategory] = useState<AdminSiteCategory | null>(null);
  const [editSlug, setEditSlug] = useState("");
  const [editName, setEditName] = useState("");
  const [editSortOrder, setEditSortOrder] = useState("0");
  const [editActive, setEditActive] = useState(true);

  const settingsQuery = useQuery({
    queryKey: ["admin-site-settings"],
    queryFn: () => adminApi.getSiteSettings(request),
  });
  const categoriesQuery = useQuery({
    queryKey: ["admin-site-categories"],
    queryFn: () => adminApi.listSiteCategories(request),
  });

  useEffect(() => {
    if (!settingsQuery.data) {
      return;
    }
    setSiteTitle(settingsQuery.data.site_title ?? "");
    setSiteDescription(settingsQuery.data.site_description ?? "");
    setRegisterEnabled(settingsQuery.data.register_enabled ?? true);
    setSiteLogoURL(settingsQuery.data.site_logo_url ?? "");
    setYTDLPParamMode(settingsQuery.data.ytdlp_param_mode === "advanced" ? "advanced" : "safe");
    setYTDLPFormat(settingsQuery.data.ytdlp_safe?.format ?? "");
    setYTDLPExtractorArgs(settingsQuery.data.ytdlp_safe?.extractor_args ?? "");
    setYTDLPUserAgent(settingsQuery.data.ytdlp_safe?.user_agent ?? "");
    setYTDLPReferer(settingsQuery.data.ytdlp_safe?.referer ?? "");
    setYTDLPHeadersText(headersToText(settingsQuery.data.ytdlp_safe?.headers));
    setYTDLPSocketTimeout(
      typeof settingsQuery.data.ytdlp_safe?.socket_timeout === "number" && settingsQuery.data.ytdlp_safe.socket_timeout > 0
        ? String(settingsQuery.data.ytdlp_safe.socket_timeout)
        : "",
    );
    setYTDLPMetadataArgsRaw(settingsQuery.data.ytdlp_metadata_args_raw ?? "");
    setYTDLPDownloadArgsRaw(settingsQuery.data.ytdlp_download_args_raw ?? "");
  }, [settingsQuery.data]);

  const patchSettingsMutation = useMutation({
    mutationFn: (payload: {
      site_title?: string;
      site_description?: string;
      register_enabled?: boolean;
      site_logo_media_id?: string;
      ytdlp_param_mode?: "safe" | "advanced";
      ytdlp_safe?: {
        format?: string;
        extractor_args?: string;
        user_agent?: string;
        referer?: string;
        headers?: Record<string, string>;
        socket_timeout?: number;
      };
      ytdlp_metadata_args_raw?: string;
      ytdlp_download_args_raw?: string;
    }) =>
      adminApi.patchSiteSettings(request, payload),
    onSuccess: async (data) => {
      setSaveMessage("站点设置已保存");
      setSaveError("");
      setSiteLogoURL(data.site_logo_url ?? "");
      await queryClient.invalidateQueries({ queryKey: ["admin-site-settings"] });
      await queryClient.invalidateQueries({ queryKey: ["site-settings-public"] });
    },
    onError: (error) => {
      setSaveError(error instanceof Error ? error.message : "保存失败");
      setSaveMessage("");
    },
  });

  const createCategoryMutation = useMutation({
    mutationFn: () =>
      adminApi.createSiteCategory(request, {
        slug: createSlug.trim().toLowerCase(),
        name: createName.trim(),
        sort_order: Number.isFinite(Number(createSortOrder)) ? Number(createSortOrder) : 0,
        is_active: createActive,
      }),
    onSuccess: async () => {
      setCreateSlug("");
      setCreateName("");
      setCreateSortOrder("0");
      setCreateActive(true);
      setCategoryError("");
      await queryClient.invalidateQueries({ queryKey: ["admin-site-categories"] });
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: (error) => {
      setCategoryError(error instanceof Error ? error.message : "新增分类失败");
    },
  });

  const patchCategoryMutation = useMutation({
    mutationFn: ({ id, payload }: { id: number; payload: { slug?: string; name?: string; sort_order?: number; is_active?: boolean } }) =>
      adminApi.patchSiteCategory(request, id, payload),
    onSuccess: async () => {
      setEditingCategory(null);
      setCategoryError("");
      await queryClient.invalidateQueries({ queryKey: ["admin-site-categories"] });
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: (error) => {
      setCategoryError(error instanceof Error ? error.message : "更新分类失败");
    },
  });

  const deleteCategoryMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteSiteCategory(request, id),
    onSuccess: async () => {
      setCategoryError("");
      await queryClient.invalidateQueries({ queryKey: ["admin-site-categories"] });
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: (error) => {
      setCategoryError(error instanceof Error ? error.message : "删除分类失败");
    },
  });

  const categories = useMemo(() => categoriesQuery.data?.items ?? [], [categoriesQuery.data?.items]);

  const uploadSiteLogo = async (file: File): Promise<string> => {
    if (!ALLOWED_LOGO_TYPES.has(file.type)) {
      throw new Error("LOGO 仅支持 JPG/PNG/WEBP");
    }
    if (file.size <= 0 || file.size > MAX_LOGO_SIZE) {
      throw new Error("LOGO 大小不能超过 10MB");
    }

    const ticketRaw = await request<UploadTicket>("/uploads/presign", {
      method: "POST",
      auth: true,
      body: {
        purpose: "cover",
        filename: file.name,
        content_type: file.type,
        file_size_bytes: file.size,
      },
    });
    const ticket = mapUploadTicket(ticketRaw);
    await uploadBinary(ticket.upload_url, file, ticket.headers);

    const completedRaw = await request<UploadCompleteData>(`/uploads/${ticket.upload_id}/complete`, {
      method: "POST",
      auth: true,
      body: {
        checksum_sha256: "",
        duration_sec: 0,
        width: 0,
        height: 0,
      },
    });
    const completed = mapUploadCompleteData(completedRaw);
    return completed.media_object_id;
  };

  const onSelectLogo = async (file?: File | null) => {
    if (!file) {
      return;
    }
    setLogoBusy(true);
    setSaveError("");
    setSaveMessage("");
    try {
      const mediaID = await uploadSiteLogo(file);
      await patchSettingsMutation.mutateAsync({ site_logo_media_id: mediaID });
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "上传 LOGO 失败");
    } finally {
      setLogoBusy(false);
    }
  };

  const onRemoveLogo = async () => {
    setLogoBusy(true);
    setSaveError("");
    setSaveMessage("");
    try {
      await patchSettingsMutation.mutateAsync({ site_logo_media_id: "" });
      setSiteLogoURL("");
    } finally {
      setLogoBusy(false);
    }
  };

  const onSaveBranding = async () => {
    setSaveError("");
    setSaveMessage("");
    await patchSettingsMutation.mutateAsync({
      site_title: siteTitle.trim(),
      site_description: siteDescription.trim(),
      register_enabled: registerEnabled,
    });
  };

  const onSaveYTDLPSettings = async () => {
    setSaveError("");
    setSaveMessage("");
    try {
      const payload: {
        ytdlp_param_mode: "safe" | "advanced";
        ytdlp_safe?: {
          format?: string;
          extractor_args?: string;
          user_agent?: string;
          referer?: string;
          headers?: Record<string, string>;
          socket_timeout?: number;
        };
        ytdlp_metadata_args_raw?: string;
        ytdlp_download_args_raw?: string;
      } = {
        ytdlp_param_mode: ytdlpParamMode,
      };

      if (ytdlpParamMode === "safe") {
        const timeout = ytdlpSocketTimeout.trim();
        const socketTimeout = timeout === "" ? 0 : Number(timeout);
        if (!Number.isFinite(socketTimeout) || socketTimeout < 0) {
          throw new Error("socket_timeout 必须是非负数字");
        }
        payload.ytdlp_safe = {
          format: ytdlpFormat.trim(),
          extractor_args: ytdlpExtractorArgs.trim(),
          user_agent: ytdlpUserAgent.trim(),
          referer: ytdlpReferer.trim(),
          headers: parseHeadersText(ytdlpHeadersText),
          socket_timeout: socketTimeout,
        };
      } else {
        payload.ytdlp_metadata_args_raw = ytdlpMetadataArgsRaw.trim();
        payload.ytdlp_download_args_raw = ytdlpDownloadArgsRaw.trim();
      }

      await patchSettingsMutation.mutateAsync(payload);
      setSaveMessage("yt-dlp 参数已保存（仅新任务生效）");
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "保存 yt-dlp 参数失败");
    }
  };

  const openEditDialog = (item: AdminSiteCategory) => {
    setEditingCategory(item);
    setEditSlug(item.slug);
    setEditName(item.name);
    setEditSortOrder(String(item.sort_order));
    setEditActive(item.is_active);
  };

  const onConfirmEdit = async () => {
    if (!editingCategory) {
      return;
    }
    await patchCategoryMutation.mutateAsync({
      id: editingCategory.id,
      payload: {
        slug: editSlug.trim().toLowerCase(),
        name: editName.trim(),
        sort_order: Number.isFinite(Number(editSortOrder)) ? Number(editSortOrder) : 0,
        is_active: editActive,
      },
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold">Site Settings</h2>
        <p className="text-sm text-slate-500">管理站点品牌信息、注册开关与分类配置。</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Branding</CardTitle>
          <CardDescription>设置站点 LOGO、标题和描述。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {settingsQuery.isLoading ? <p className="text-sm text-slate-500">加载设置中...</p> : null}
          <div className="flex items-start gap-4">
            <div className="h-16 w-16 overflow-hidden rounded-lg border border-slate-200 bg-slate-100">
              {siteLogoURL ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={siteLogoURL} alt={siteTitle || "logo"} className="h-full w-full object-cover" />
              ) : null}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <label className="inline-flex cursor-pointer items-center">
                <input
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  className="hidden"
                  onChange={(event) => void onSelectLogo(event.target.files?.[0])}
                />
                <span className="rounded-md border border-slate-200 px-3 py-2 text-sm hover:border-primary/30 hover:text-primary">
                  {logoBusy ? "上传中..." : "上传 LOGO"}
                </span>
              </label>
              <Button type="button" variant="outline" disabled={logoBusy || !siteLogoURL} onClick={() => void onRemoveLogo()}>
                移除 LOGO
              </Button>
            </div>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">站点标题</label>
              <Input value={siteTitle} onChange={(event) => setSiteTitle(event.target.value)} />
            </div>
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">注册开关</label>
              <Select value={registerEnabled ? "enabled" : "disabled"} onValueChange={(value) => setRegisterEnabled(value === "enabled")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="enabled">启用注册</SelectItem>
                  <SelectItem value="disabled">关闭注册</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs font-semibold text-slate-500">站点描述</label>
            <Textarea value={siteDescription} onChange={(event) => setSiteDescription(event.target.value)} rows={4} />
          </div>

          {saveError ? <p className="text-xs text-rose-600">{saveError}</p> : null}
          {saveMessage ? <p className="text-xs text-emerald-600">{saveMessage}</p> : null}

          <div className="flex justify-end">
            <Button onClick={() => void onSaveBranding()} disabled={patchSettingsMutation.isPending || settingsQuery.isLoading}>
              {patchSettingsMutation.isPending ? "保存中..." : "保存设置"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Import / yt-dlp</CardTitle>
          <CardDescription>配置 URL 导入使用的 yt-dlp 参数。仅对新建 URL 导入任务生效。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-semibold text-slate-500">参数模式</label>
            <Select value={ytdlpParamMode} onValueChange={(value) => setYTDLPParamMode(value === "advanced" ? "advanced" : "safe")}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="safe">安全模式（白名单字段）</SelectItem>
                <SelectItem value="advanced">高级模式（原始参数）</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {ytdlpParamMode === "safe" ? (
            <div className="grid gap-3 md:grid-cols-2">
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">Format</label>
                <Input value={ytdlpFormat} onChange={(event) => setYTDLPFormat(event.target.value)} placeholder="bestvideo+bestaudio/best" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">Extractor Args</label>
                <Input value={ytdlpExtractorArgs} onChange={(event) => setYTDLPExtractorArgs(event.target.value)} />
              </div>
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">User Agent</label>
                <Input value={ytdlpUserAgent} onChange={(event) => setYTDLPUserAgent(event.target.value)} />
              </div>
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">Referer</label>
                <Input value={ytdlpReferer} onChange={(event) => setYTDLPReferer(event.target.value)} />
              </div>
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">Socket Timeout（秒）</label>
                <Input value={ytdlpSocketTimeout} onChange={(event) => setYTDLPSocketTimeout(event.target.value)} placeholder="0" type="number" />
              </div>
              <div className="md:col-span-2">
                <label className="mb-1 block text-xs font-semibold text-slate-500">Headers（每行 `Key: Value`）</label>
                <Textarea value={ytdlpHeadersText} onChange={(event) => setYTDLPHeadersText(event.target.value)} rows={4} />
              </div>
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">Metadata Args Raw</label>
                <Textarea
                  value={ytdlpMetadataArgsRaw}
                  onChange={(event) => setYTDLPMetadataArgsRaw(event.target.value)}
                  rows={5}
                  placeholder='例如：--extractor-args "generic:foo=bar"'
                />
              </div>
              <div>
                <label className="mb-1 block text-xs font-semibold text-slate-500">Download Args Raw</label>
                <Textarea
                  value={ytdlpDownloadArgsRaw}
                  onChange={(event) => setYTDLPDownloadArgsRaw(event.target.value)}
                  rows={5}
                  placeholder="例如：--format best"
                />
              </div>
            </div>
          )}

          <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
            说明：配置只会作用于新创建的 URL 导入任务，已排队或运行中的任务不会被修改。
          </div>

          <div className="flex justify-end">
            <Button onClick={() => void onSaveYTDLPSettings()} disabled={patchSettingsMutation.isPending || settingsQuery.isLoading}>
              {patchSettingsMutation.isPending ? "保存中..." : "保存 yt-dlp 配置"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Categories</CardTitle>
          <CardDescription>新增、编辑与删除视频分类。被视频引用的分类不可删除。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-2 rounded-lg border border-slate-200 p-3 md:grid-cols-5">
            <Input placeholder="slug" value={createSlug} onChange={(event) => setCreateSlug(event.target.value)} />
            <Input placeholder="名称" value={createName} onChange={(event) => setCreateName(event.target.value)} />
            <Input
              placeholder="排序"
              value={createSortOrder}
              onChange={(event) => setCreateSortOrder(event.target.value)}
              type="number"
            />
            <Select value={createActive ? "active" : "inactive"} onValueChange={(value) => setCreateActive(value === "active")}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="active">启用</SelectItem>
                <SelectItem value="inactive">停用</SelectItem>
              </SelectContent>
            </Select>
            <Button
              onClick={() => void createCategoryMutation.mutateAsync()}
              disabled={createCategoryMutation.isPending || !createSlug.trim() || !createName.trim()}
            >
              {createCategoryMutation.isPending ? "新增中..." : "新增分类"}
            </Button>
          </div>

          {categoryError ? <p className="text-xs text-rose-600">{categoryError}</p> : null}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Slug</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>排序</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {categories.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>{item.id}</TableCell>
                  <TableCell>{item.slug}</TableCell>
                  <TableCell>{item.name}</TableCell>
                  <TableCell>{item.sort_order}</TableCell>
                  <TableCell>{item.is_active ? "active" : "inactive"}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-2">
                      <Button size="sm" variant="outline" onClick={() => openEditDialog(item)}>
                        编辑
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => {
                          if (!window.confirm(`确认删除分类 ${item.name} 吗？`)) {
                            return;
                          }
                          void deleteCategoryMutation.mutateAsync(item.id);
                        }}
                        disabled={deleteCategoryMutation.isPending}
                      >
                        删除
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={!!editingCategory} onOpenChange={(open) => (!open ? setEditingCategory(null) : null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑分类</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">Slug</label>
              <Input value={editSlug} onChange={(event) => setEditSlug(event.target.value)} />
            </div>
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">名称</label>
              <Input value={editName} onChange={(event) => setEditName(event.target.value)} />
            </div>
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">排序</label>
              <Input value={editSortOrder} onChange={(event) => setEditSortOrder(event.target.value)} type="number" />
            </div>
            <div>
              <label className="mb-1 block text-xs font-semibold text-slate-500">状态</label>
              <Select value={editActive ? "active" : "inactive"} onValueChange={(value) => setEditActive(value === "active")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="active">启用</SelectItem>
                  <SelectItem value="inactive">停用</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditingCategory(null)}>
              取消
            </Button>
            <Button onClick={() => void onConfirmEdit()} disabled={patchCategoryMutation.isPending}>
              {patchCategoryMutation.isPending ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
