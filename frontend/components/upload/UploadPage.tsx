"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { useAuth } from "@/components/auth/AuthProvider";
import { AppIcon } from "@/components/common/AppIcon";
import { EmptyState } from "@/components/common/EmptyState";
import type { Category, UploadCompleteData, UploadTicket } from "@/lib/dto";
import { mapCategory, mapUploadCompleteData, mapUploadTicket } from "@/lib/dto/mappers";

const MAX_BYTES = 2 * 1024 * 1024 * 1024;
const ALLOWED_VIDEO_MIMES = new Set([
  "video/mp4",
  "video/quicktime",
  "video/x-msvideo",
  "video/webm",
  "video/x-matroska",
  "video/matroska",
  "video/mkv",
  "video/x-mkv",
  "application/x-matroska",
  "application/matroska",
  "video/x-flv",
  "video/mpeg",
  "video/3gpp",
  "video/x-m4v",
  "video/mp2t",
]);
const ALLOWED_VIDEO_EXTENSIONS = new Set([
  ".mp4",
  ".mov",
  ".avi",
  ".webm",
  ".mkv",
  ".flv",
  ".mpeg",
  ".mpg",
  ".3gp",
  ".m4v",
  ".ts",
]);
const VIDEO_MIME_BY_EXTENSION: Record<string, string> = {
  ".mp4": "video/mp4",
  ".mov": "video/quicktime",
  ".avi": "video/x-msvideo",
  ".webm": "video/webm",
  ".mkv": "video/x-matroska",
  ".flv": "video/x-flv",
  ".mpeg": "video/mpeg",
  ".mpg": "video/mpeg",
  ".3gp": "video/3gpp",
  ".m4v": "video/x-m4v",
  ".ts": "video/mp2t",
};

type UploadPurpose = "video" | "cover";

function getFileExtension(filename: string): string {
  const idx = filename.lastIndexOf(".");
  if (idx < 0) {
    return "";
  }
  return filename.slice(idx).toLowerCase();
}

function resolveVideoContentType(file: File): string {
  const fileType = file.type.toLowerCase().trim();
  if (ALLOWED_VIDEO_MIMES.has(fileType)) {
    return fileType;
  }
  const ext = getFileExtension(file.name);
  if (ext && VIDEO_MIME_BY_EXTENSION[ext]) {
    return VIDEO_MIME_BY_EXTENSION[ext];
  }
  return fileType || "application/octet-stream";
}

export function UploadPage() {
  const router = useRouter();
  const { ready, request, uploadBinary, session, openAuthDialog } = useAuth();

  const [categories, setCategories] = useState<Category[]>([]);
  const [loadingCategories, setLoadingCategories] = useState(true);
  const [categoriesError, setCategoriesError] = useState("");

  const [videoFile, setVideoFile] = useState<File | null>(null);
  const [coverFile, setCoverFile] = useState<File | null>(null);
  const [coverPreviewUrl, setCoverPreviewUrl] = useState<string | null>(null);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [tagInput, setTagInput] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [categoryId, setCategoryId] = useState("");
  const [visibility, setVisibility] = useState<"public" | "unlisted" | "private">("public");

  const [submitting, setSubmitting] = useState(false);
  const [statusText, setStatusText] = useState("");
  const [errorText, setErrorText] = useState("");

  const videoInputRef = useRef<HTMLInputElement>(null);
  const coverInputRef = useRef<HTMLInputElement>(null);

  const fieldClass =
    "w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-900 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary disabled:cursor-not-allowed disabled:opacity-70";

  const loadCategories = useCallback(async () => {
    setLoadingCategories(true);
    setCategoriesError("");
    try {
      const data = await request<Category[]>("/categories", { auth: false });
      setCategories((data ?? []).map(mapCategory));
    } catch (err) {
      setCategoriesError(err instanceof Error ? err.message : "加载分类失败");
      setCategories([]);
    } finally {
      setLoadingCategories(false);
    }
  }, [request]);

  useEffect(() => {
    void loadCategories();
  }, [loadCategories]);

  useEffect(() => {
    if (!coverFile) {
      setCoverPreviewUrl(null);
      return;
    }
    const url = URL.createObjectURL(coverFile);
    setCoverPreviewUrl(url);
    return () => {
      URL.revokeObjectURL(url);
    };
  }, [coverFile]);

  const ensureAuth = (): boolean => {
    if (session) {
      return true;
    }
    openAuthDialog("login");
    return false;
  };

  const addTag = (raw: string) => {
    const tag = raw.trim();
    if (!tag) {
      return;
    }
    if (tags.includes(tag)) {
      return;
    }
    if (tags.length >= 10) {
      return;
    }
    setTags((prev) => [...prev, tag]);
  };

  const removeTag = (value: string) => {
    setTags((prev) => prev.filter((tag) => tag !== value));
  };

  const validateFile = (file: File, purpose: UploadPurpose): string | null => {
    if (!file) {
      return `${purpose === "video" ? "视频" : "封面"}文件不能为空`;
    }
    if (file.size <= 0 || file.size > MAX_BYTES) {
      return `${purpose === "video" ? "视频" : "封面"}大小不合法（最大 2GB）`;
    }

    if (purpose === "video") {
      const fileType = file.type.toLowerCase().trim();
      const ext = getFileExtension(file.name);
      const mimeAllowed = ALLOWED_VIDEO_MIMES.has(fileType);
      const extFallbackAllowed = ALLOWED_VIDEO_EXTENSIONS.has(ext);
      if (!mimeAllowed && !extFallbackAllowed) {
        return "视频格式不支持，仅支持 MP4/MOV/AVI/WEBM/MKV/FLV/MPEG/MPG/3GP/M4V/TS";
      }
      return null;
    }

    const allowed = ["image/jpeg", "image/png", "image/webp"];
    if (!allowed.includes(file.type)) {
      return "封面格式不支持，仅支持 JPG/PNG/WEBP";
    }
    return null;
  };

  const uploadMedia = async (file: File, purpose: UploadPurpose): Promise<UploadCompleteData> => {
    const issue = validateFile(file, purpose);
    if (issue) {
      throw new Error(issue);
    }

    setStatusText(`申请${purpose === "video" ? "视频" : "封面"}上传凭证...`);
    const ticketRaw = await request<UploadTicket>("/uploads/presign", {
      method: "POST",
      auth: true,
      body: {
        purpose,
        filename: file.name,
        content_type: purpose === "video" ? resolveVideoContentType(file) : file.type,
        file_size_bytes: file.size,
      },
    });
    const ticket = mapUploadTicket(ticketRaw);

    setStatusText(`上传${purpose === "video" ? "视频" : "封面"}文件...`);
    await uploadBinary(ticket.upload_url, file, ticket.headers);

    setStatusText(`确认${purpose === "video" ? "视频" : "封面"}上传完成...`);
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
    return mapUploadCompleteData(completedRaw);
  };

  const onPublish = async () => {
    if (!ensureAuth()) {
      return;
    }

    if (!videoFile) {
      setErrorText("请先选择一个视频文件");
      return;
    }
    if (!title.trim()) {
      setErrorText("请填写视频标题");
      return;
    }

    setSubmitting(true);
    setErrorText("");

    try {
      const videoMedia = await uploadMedia(videoFile, "video");
      const coverMedia = coverFile ? await uploadMedia(coverFile, "cover") : null;

      setStatusText("发布视频中...");
      const created = await request<{ id: string }>("/videos", {
        method: "POST",
        auth: true,
        body: {
          title: title.trim(),
          description: description.trim(),
          category_id: categoryId ? Number(categoryId) : null,
          cover_media_id: coverMedia?.media_object_id,
          source_media_id: videoMedia.media_object_id,
          tags,
          visibility,
        },
      });

      setStatusText("发布成功，正在跳转...");
      router.push(`/videos/${created.id}`);
    } catch (err) {
      setErrorText(err instanceof Error ? err.message : "发布失败");
      setStatusText("");
    } finally {
      setSubmitting(false);
    }
  };

  if (!ready) {
    return (
      <div className="mx-auto w-full max-w-5xl rounded-xl border border-primary/10 bg-white p-10 shadow-sm">
        <div className="space-y-4">
          <div className="h-8 w-48 animate-pulse rounded-lg bg-slate-100" />
          <div className="h-5 w-72 animate-pulse rounded-lg bg-slate-100" />
          <div className="h-11 w-28 animate-pulse rounded-full bg-slate-100" />
        </div>
      </div>
    );
  }

  if (!session) {
    return (
      <div className="mx-auto w-full max-w-5xl rounded-xl border border-primary/10 bg-white p-10 shadow-sm">
        <EmptyState title="请先登录后再上传" description="上传中心需要登录态，登录后可发布视频与封面。" />
        <div className="mt-6 flex justify-center">
          <button
            type="button"
            onClick={() => openAuthDialog("login")}
            className="rounded-full bg-primary px-6 py-2.5 text-sm font-bold text-white transition-all hover:bg-primary/90"
          >
            立即登录
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-5xl rounded-xl border border-primary/5 bg-white p-6 shadow-sm md:p-10">
      <div className="mb-10 flex flex-col">
        <div
          className="group flex cursor-pointer flex-col items-center gap-6 rounded-xl border-2 border-dashed border-primary/30 px-6 py-16 transition-colors hover:bg-primary/5"
          onDragOver={(event) => {
            event.preventDefault();
          }}
          onDrop={(event) => {
            event.preventDefault();
            const file = event.dataTransfer.files[0];
            if (file) {
              setVideoFile(file);
            }
          }}
          onClick={() => videoInputRef.current?.click()}
        >
          <div className="flex size-20 items-center justify-center rounded-full bg-primary/10 transition-transform group-hover:scale-110">
            <AppIcon name="cloud_upload" size={48} className="text-primary" />
          </div>
          <div className="flex flex-col items-center gap-2">
            <p className="text-center text-xl font-bold text-slate-900">点击或拖拽视频至此上传</p>
            <p className="text-center text-sm text-slate-500">支持 MP4, MOV, AVI, WEBM，文件大小不超过 2GB</p>
            <p className="text-center text-xs text-slate-400">
              也支持 MKV / FLV / MPEG / MPG / 3GP / M4V / TS
            </p>
            {videoFile ? <p className="text-sm font-semibold text-primary">已选择：{videoFile.name}</p> : null}
          </div>
          <button
            type="button"
            className="flex h-12 min-w-[140px] items-center justify-center rounded-full bg-primary px-6 text-sm font-bold text-white shadow-lg shadow-primary/30 transition-all hover:shadow-xl active:scale-95"
          >
            选择视频文件
          </button>
        </div>
        <input
          ref={videoInputRef}
          className="hidden"
          type="file"
          accept="video/mp4,video/quicktime,video/x-msvideo,video/webm,video/x-matroska,video/matroska,video/mkv,video/x-mkv,application/x-matroska,application/matroska,video/x-flv,video/mpeg,video/3gpp,video/x-m4v,video/mp2t,.mp4,.mov,.avi,.webm,.mkv,.flv,.mpeg,.mpg,.3gp,.m4v,.ts"
          onChange={(event) => setVideoFile(event.target.files?.[0] ?? null)}
        />
      </div>

      <div className="grid grid-cols-1 gap-10 lg:grid-cols-3">
        <div className="space-y-6 lg:col-span-2">
          <h3 className="flex items-center gap-2 text-lg font-bold text-slate-900">
            <AppIcon name="edit_note" className="text-primary" />
            基础信息
          </h3>

          <div className="space-y-4">
            <label className="block">
              <span className="mb-2 ml-1 block text-sm font-bold text-slate-700">视频标题</span>
              <input
                className={fieldClass}
                placeholder="起一个超级吸睛的标题吧~"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
              />
            </label>

            <label className="block">
              <span className="mb-2 ml-1 block text-sm font-bold text-slate-700">视频描述</span>
              <textarea
                className={fieldClass}
                placeholder="详细介绍下你的视频内容，让更多人发现它..."
                rows={4}
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </label>

            <label className="block">
              <span className="mb-2 ml-1 block text-sm font-bold text-slate-700">添加标签</span>
              <div className="mb-2 flex flex-wrap gap-2">
                {tags.map((tag) => (
                  <span
                    key={tag}
                    className="flex items-center gap-1 rounded-full bg-primary/10 px-3 py-1 text-xs font-bold text-primary"
                  >
                    {tag}
                    <button type="button" onClick={() => removeTag(tag)}>
                      <AppIcon name="close" size={14} className="cursor-pointer" />
                    </button>
                  </span>
                ))}
              </div>

              <div className="relative">
                <input
                  className={`${fieldClass} pl-10`}
                  placeholder="输入标签按回车添加"
                  value={tagInput}
                  onChange={(event) => setTagInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      addTag(tagInput);
                      setTagInput("");
                    }
                  }}
                />
                <AppIcon name="tag" className="absolute left-3 top-3.5 text-slate-400" />
              </div>
            </label>
          </div>
        </div>

        <div className="space-y-6">
          <div>
            <h3 className="mb-4 flex items-center gap-2 text-lg font-bold text-slate-900">
              <AppIcon name="image" className="text-primary" />
              封面设置
            </h3>

            <div
              className="group relative flex aspect-video w-full cursor-pointer flex-col items-center justify-center gap-2 overflow-hidden rounded-xl border-2 border-dashed border-primary/30 bg-primary/5"
              onClick={() => coverInputRef.current?.click()}
            >
              {coverFile && coverPreviewUrl ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={coverPreviewUrl}
                  alt="封面预览"
                  className="absolute inset-0 h-full w-full object-cover"
                />
              ) : (
                <>
                  <AppIcon
                    name="add_photo_alternate"
                    size={36}
                    className="text-primary/40 transition-colors group-hover:text-primary"
                  />
                  <span className="text-xs font-medium text-primary/60">点击上传自定义封面</span>
                </>
              )}
              <div className="absolute inset-0 flex items-center justify-center bg-slate-900/40 opacity-0 transition-opacity group-hover:opacity-100">
                <span className="text-sm font-bold text-white">更换封面</span>
              </div>
            </div>
            <input
              ref={coverInputRef}
              className="hidden"
              type="file"
              accept="image/jpeg,image/png,image/webp"
              onChange={(event) => setCoverFile(event.target.files?.[0] ?? null)}
            />
          </div>

          <div>
            <span className="mb-2 ml-1 block text-sm font-bold text-slate-700">全部分类</span>
            <select
              className={`${fieldClass} appearance-none`}
              value={categoryId}
              onChange={(event) => setCategoryId(event.target.value)}
              disabled={loadingCategories}
            >
              <option value="">请选择分类</option>
              {categories.map((category) => (
                <option key={category.id} value={category.id}>
                  {category.name}
                </option>
              ))}
            </select>
            {categoriesError ? <p className="mt-2 text-xs text-rose-500">{categoriesError}</p> : null}
          </div>

          <div>
            <span className="mb-2 ml-1 block text-sm font-bold text-slate-700">可见性</span>
            <select
              className={`${fieldClass} appearance-none`}
              value={visibility}
              onChange={(event) => setVisibility(event.target.value as "public" | "unlisted" | "private")}
            >
              <option value="public">公开（所有人可见）</option>
              <option value="unlisted">非公开列表（仅持链接可访问）</option>
              <option value="private">私密（仅自己可见）</option>
            </select>
          </div>

          <div className="rounded-xl border border-primary/10 bg-primary/5 p-4">
            <h4 className="mb-2 flex items-center gap-1 text-xs font-bold text-primary">
              <AppIcon name="info" size={12} />
              上传小贴士
            </h4>
            <p className="text-xs leading-relaxed text-slate-500">
              使用高质量封面能显著提高点击率。建议封面比例 16:9，分辨率不低于 1280x720。
            </p>
          </div>
        </div>
      </div>

      <div className="mt-12 flex flex-col items-center justify-end gap-4 border-t border-slate-100 pt-8 sm:flex-row">
        {statusText ? <p className="w-full text-sm font-medium text-primary sm:mr-auto sm:w-auto">{statusText}</p> : null}
        {errorText ? <p className="w-full text-sm font-medium text-rose-500 sm:mr-auto sm:w-auto">{errorText}</p> : null}

        <button
          type="button"
          className="w-full rounded-full px-10 py-3 text-sm font-bold text-slate-500 transition-colors hover:bg-slate-100 sm:w-auto"
          onClick={() => {
            setVideoFile(null);
            setCoverFile(null);
            setTitle("");
            setDescription("");
            setTagInput("");
            setTags([]);
            setCategoryId("");
            setVisibility("public");
            setErrorText("");
            setStatusText("");
          }}
          disabled={submitting}
        >
          取消
        </button>

        <button
          type="button"
          className="w-full rounded-full bg-primary px-12 py-3 text-sm font-bold text-white shadow-lg shadow-primary/30 transition-all hover:shadow-xl hover:-translate-y-0.5 disabled:cursor-not-allowed disabled:opacity-60 sm:w-auto"
          onClick={() => void onPublish()}
          disabled={submitting}
        >
          {submitting ? "发布中..." : "立即发布"}
        </button>
      </div>
    </div>
  );
}
