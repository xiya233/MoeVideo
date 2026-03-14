import type { ReactNode } from "react";

import { SiteShell } from "@/components/layout/SiteShell";
import { getPublicSiteSettingsServer } from "@/lib/site-settings/server";

export default async function SiteLayout({ children }: { children: ReactNode }) {
  const initialSiteSettings = await getPublicSiteSettingsServer();
  return <SiteShell initialSiteSettings={initialSiteSettings}>{children}</SiteShell>;
}
