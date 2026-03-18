import type { Metadata } from "next";
import Script from "next/script";
import type { ReactNode } from "react";

import { AuthProvider } from "@/components/auth/AuthProvider";
import { QueryProvider } from "@/components/providers/QueryProvider";
import "./globals.css";

export const metadata: Metadata = {
  title: "MoeVideo",
  description: "MoeVideo VOD - Stitch design implementation",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <head>
        <link
          href="https://fonts.googleapis.com/css2?family=Spline+Sans:wght@300;400;500;600;700&family=Noto+Sans+SC:wght@400;500;700&display=swap"
          rel="stylesheet"
        />
        <link href="https://unpkg.com/artplayer/dist/artplayer.css" rel="stylesheet" />
      </head>
      <body>
        <Script src="/runtime-env.js" strategy="beforeInteractive" />
        <QueryProvider>
          <AuthProvider>{children}</AuthProvider>
        </QueryProvider>
      </body>
    </html>
  );
}
