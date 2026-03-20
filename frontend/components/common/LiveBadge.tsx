"use client";

import { cn } from "@/lib/utils/cn";

export function LiveBadge({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full bg-rose-600/95 px-2 py-1 text-[10px] font-black uppercase tracking-wide text-white shadow-sm shadow-rose-800/40",
        className,
      )}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-white" />
      Live
    </span>
  );
}
