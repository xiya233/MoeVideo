export default function UploadPage() {
  return (
    <main className="space-y-6">
      <header>
        <h1 className="text-2xl font-bold text-slate-900">上传中心</h1>
        <p className="mt-2 text-sm text-slate-600">首版发布流程：presign -&gt; 直传(local/s3) -&gt; complete -&gt; POST /videos</p>
      </header>

      <section className="rounded-2xl border border-slate-200 bg-white p-6">
        <ol className="list-decimal space-y-2 pl-5 text-sm text-slate-700">
          <li>调用 <code>POST /api/v1/uploads/presign</code> 获取上传票据。</li>
          <li>按票据方法上传文件（local: <code>PUT /uploads/local/:uploadToken</code>，s3: 预签名 URL）。</li>
          <li>调用 <code>POST /api/v1/uploads/:uploadId/complete</code> 固化媒体对象。</li>
          <li>调用 <code>POST /api/v1/videos</code> 发布视频（无草稿，立即发布）。</li>
        </ol>
      </section>
    </main>
  );
}
