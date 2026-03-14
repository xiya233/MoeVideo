import { Suspense } from "react";

import { TagsPage } from "@/components/tags/TagsPage";

export default function TagsRoute() {
  return (
    <Suspense fallback={null}>
      <TagsPage />
    </Suspense>
  );
}
