import { UserProfilePage } from "@/components/profile/UserProfilePage";

export default async function PublicProfilePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <UserProfilePage userId={id} />;
}
