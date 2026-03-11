export function EmptyState({
  title,
  description,
}: {
  title: string;
  description?: string;
}) {
  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-sky-200 bg-white/80 px-6 py-12 text-center">
      <span className="material-symbols-outlined mb-3 text-4xl text-sky-400">sentiment_neutral</span>
      <h3 className="text-base font-bold text-slate-800">{title}</h3>
      {description ? <p className="mt-2 text-sm text-slate-500">{description}</p> : null}
    </div>
  );
}
