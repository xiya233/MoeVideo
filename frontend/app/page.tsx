import Link from "next/link";
import { apiGet } from "@/lib/api";

type VideoCard = {
  id: string;
  title: string;
  cover_url?: string;
  views_count: number;
  comments_count: number;
  published_at: string;
  author: {
    id: string;
    username: string;
  };
};

type HomePayload = {
  featured: VideoCard | null;
  hot_rankings: VideoCard[];
  videos: VideoCard[];
};

export default async function HomePage() {
  const home = await apiGet<HomePayload>("/home");

  return (
    <main className="space-y-8">
      <header className="rounded-2xl border border-sky-100 bg-white/70 p-6 shadow-sm backdrop-blur">
        <h1 className="text-3xl font-bold tracking-tight text-slate-900">MoeVideo</h1>
        <p className="mt-2 text-sm text-slate-600">Next.js App Router + Fiber + SQLite WAL</p>
      </header>

      <section className="rounded-2xl border border-sky-100 bg-white/80 p-6">
        <h2 className="text-lg font-semibold text-slate-900">今日推荐</h2>
        {home?.featured ? (
          <Link className="mt-3 block text-sky-600 hover:underline" href={`/videos/${home.featured.id}`}>
            {home.featured.title}
          </Link>
        ) : (
          <p className="mt-3 text-sm text-slate-500">暂无推荐内容</p>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-semibold text-slate-900">视频列表</h2>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {(home?.videos ?? []).map((video) => (
            <article key={video.id} className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
              <h3 className="line-clamp-2 text-sm font-semibold text-slate-900">{video.title}</h3>
              <p className="mt-2 text-xs text-slate-500">UP: {video.author?.username ?? "未知"}</p>
              <p className="text-xs text-slate-500">
                {video.views_count} 播放 · {video.comments_count} 评论
              </p>
              <Link className="mt-3 inline-block text-xs font-medium text-sky-600 hover:underline" href={`/videos/${video.id}`}>
                进入播放页
              </Link>
            </article>
          ))}
        </div>
      </section>
    </main>
  );
}
