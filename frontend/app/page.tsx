import { HomePage } from "@/components/home/HomePage";

export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ q?: string; category?: string }>;
}) {
  const params = await searchParams;
  return <HomePage query={params.q} category={params.category} />;
}
