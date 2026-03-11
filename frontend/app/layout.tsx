import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "MoeVideo",
  description: "MoeVideo VOD - Next.js + Fiber",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>
        <div className="mx-auto min-h-screen w-full max-w-6xl px-4 py-8 md:px-6">{children}</div>
      </body>
    </html>
  );
}
