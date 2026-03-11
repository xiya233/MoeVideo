import { VideoPage } from "@/components/video/VideoPage";

export default async function VideoDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <VideoPage videoId={id} />;
}
