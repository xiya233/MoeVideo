import Link from "next/link";
import { apiGet } from "@/lib/api";

type VideoDetail = {
  video: {
    id: string;
    title: string;
    author: {
      id: string;
      username: string;
    };
    views_count: number;
    comments_count: number;
    published_at: string;
  };
  source_url: string;
  description: string;
  tags: string[];
  stats: {
    likes_count: number;
    favorites_count: number;
    shares_count: number;
  };
};

export default async function VideoDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const detail = await apiGet<VideoDetail>(`/videos/${id}`);

  if (!detail) {
    return (
      <main className="space-y-4">
        <Link className="text-sm text-sky-600 hover:underline" href="/">
          返回首页
        </Link>
        <p className="text-sm text-slate-600">视频不存在或暂不可见。</p>
      </main>
    );
  }

  return (
    <main className="space-y-6">
      <Link className="text-sm text-sky-600 hover:underline" href="/">
        返回首页
      </Link>
      <section className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <h1 className="text-2xl font-bold text-slate-900">{detail.video.title}</h1>
        <p className="mt-2 text-sm text-slate-600">UP: {detail.video.author.username}</p>
        <p className="text-sm text-slate-600">
          {detail.video.views_count} 播放 · {detail.video.comments_count} 评论
        </p>
        <p className="mt-4 text-sm leading-6 text-slate-700">{detail.description}</p>
        <div className="mt-4 flex flex-wrap gap-2">
          {detail.tags.map((tag) => (
            <span key={tag} className="rounded-full bg-sky-50 px-3 py-1 text-xs text-sky-700">
              {tag}
            </span>
          ))}
        </div>
      </section>
      <section className="rounded-2xl border border-slate-200 bg-white p-6">
        <h2 className="text-base font-semibold text-slate-900">播放地址</h2>
        <code className="mt-2 block overflow-x-auto rounded-lg bg-slate-950/95 p-3 text-xs text-slate-100">{detail.source_url}</code>
      </section>
    </main>
  );
}
