import type { ReactNode } from "react";

import { AdminGuard } from "@/components/admin/AdminGuard";

export default function AdminProtectedLayout({ children }: { children: ReactNode }) {
  return <AdminGuard>{children}</AdminGuard>;
}
