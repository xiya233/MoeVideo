"use client";

import Link from "next/link";
import { useState } from "react";

import { cn } from "@/lib/utils/cn";

type AuthorInlineProps = {
  username: string;
  avatarUrl?: string;
  className?: string;
  avatarClassName?: string;
  usernameClassName?: string;
  href?: string;
};

export function AuthorInline({
  username,
  avatarUrl,
  className,
  avatarClassName,
  usernameClassName,
  href,
}: AuthorInlineProps) {
  const [imageBroken, setImageBroken] = useState(false);
  const showAvatar = Boolean(avatarUrl) && !imageBroken;
  const initial = (username || "U").slice(0, 1).toUpperCase();
  const content = (
    <>
      <div
        className={cn(
          "relative h-5 w-5 shrink-0 overflow-hidden rounded-full bg-primary/20",
          avatarClassName,
        )}
      >
        {showAvatar ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={avatarUrl}
            alt={username}
            className="h-full w-full object-cover"
            onError={() => setImageBroken(true)}
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-[10px] font-bold text-primary">
            {initial}
          </div>
        )}
      </div>
      <span className={cn("truncate text-[11px] font-medium", usernameClassName)}>{username}</span>
    </>
  );

  if (href) {
    return (
      <Link
        href={href}
        className={cn("flex min-w-0 items-center gap-2 transition-colors hover:text-primary", className)}
      >
        {content}
      </Link>
    );
  }

  return <div className={cn("flex min-w-0 items-center gap-2", className)}>{content}</div>;
}
