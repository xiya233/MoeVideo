"use client";

import { useCallback, useMemo, useState } from "react";
import Cropper, { type Area } from "react-easy-crop";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type AvatarCropDialogProps = {
  open: boolean;
  source: string;
  onOpenChange: (open: boolean) => void;
  onConfirm: (file: File, previewURL: string) => void;
};

const OUTPUT_SIZE = 512;

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("加载图片失败"));
    img.src = url;
  });
}

async function cropToWebP(source: string, crop: Area): Promise<Blob> {
  const image = await loadImage(source);
  const canvas = document.createElement("canvas");
  canvas.width = OUTPUT_SIZE;
  canvas.height = OUTPUT_SIZE;
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    throw new Error("浏览器不支持图片裁剪");
  }

  ctx.imageSmoothingEnabled = true;
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(
    image,
    crop.x,
    crop.y,
    crop.width,
    crop.height,
    0,
    0,
    OUTPUT_SIZE,
    OUTPUT_SIZE,
  );

  const blob = await new Promise<Blob | null>((resolve) => {
    canvas.toBlob(resolve, "image/webp", 0.9);
  });
  if (!blob) {
    throw new Error("导出 WebP 失败");
  }
  return blob;
}

export function AvatarCropDialog({ open, source, onOpenChange, onConfirm }: AvatarCropDialogProps) {
  const [crop, setCrop] = useState({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(1);
  const [croppedAreaPixels, setCroppedAreaPixels] = useState<Area | null>(null);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const canSubmit = useMemo(() => !!source && !!croppedAreaPixels && !submitting, [croppedAreaPixels, source, submitting]);

  const onCropComplete = useCallback((_: Area, areaPixels: Area) => {
    setCroppedAreaPixels(areaPixels);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!croppedAreaPixels) {
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const blob = await cropToWebP(source, croppedAreaPixels);
      const file = new File([blob], "avatar.webp", { type: "image/webp" });
      const preview = URL.createObjectURL(blob);
      onConfirm(file, preview);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "裁剪失败");
    } finally {
      setSubmitting(false);
    }
  }, [croppedAreaPixels, onConfirm, onOpenChange, source]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>裁剪头像</DialogTitle>
          <DialogDescription>头像将保存为 512x512 WebP。</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="relative h-[360px] w-full overflow-hidden rounded-xl bg-slate-900">
            {source ? (
              <Cropper
                image={source}
                crop={crop}
                zoom={zoom}
                aspect={1}
                cropShape="round"
                showGrid={false}
                onCropChange={setCrop}
                onZoomChange={setZoom}
                onCropComplete={onCropComplete}
                maxZoom={4}
                minZoom={1}
              />
            ) : null}
          </div>

          <div>
            <label className="mb-1 block text-xs font-medium text-slate-600">缩放</label>
            <input
              type="range"
              min={1}
              max={4}
              step={0.01}
              value={zoom}
              onChange={(event) => setZoom(Number(event.target.value))}
              className="w-full accent-primary"
            />
          </div>
          {error ? <p className="text-xs text-rose-600">{error}</p> : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            取消
          </Button>
          <Button onClick={() => void handleConfirm()} disabled={!canSubmit}>
            {submitting ? "处理中..." : "确认使用"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
