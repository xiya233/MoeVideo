export type Category = {
  id: number;
  slug: string;
  name: string;
  sort_order: number;
};

export type Author = {
  id: string;
  username: string;
  followers_count: number;
  avatar_url?: string;
  followed?: boolean;
};

export type VideoCard = {
  id: string;
  title: string;
  cover_url?: string;
  duration_sec: number;
  views_count: number;
  comments_count: number;
  published_at: string;
  category: string;
  author: Author;
};

export type HomeData = {
  featured: VideoCard | null;
  hot_rankings: VideoCard[];
  categories: Category[];
  videos: VideoCard[];
  next_cursor?: string;
};

export type VideoDetail = {
  status: "processing" | "published" | "failed";
  video: VideoCard;
  source_url: string;
  playback: {
    status: "processing" | "ready" | "failed";
    type: "" | "hls" | "mp4";
    hls_master_url?: string;
    mp4_url?: string;
    variants: Array<{
      name: string;
      width: number;
      height: number;
      bandwidth: number;
      url: string;
    }>;
  };
  description: string;
  tags: string[];
  stats: {
    views_count: number;
    likes_count: number;
    favorites_count: number;
    comments_count: number;
    shares_count: number;
  };
  uploader: Author;
  viewer_actions: {
    liked: boolean;
    favorited: boolean;
    following_uploader: boolean;
  };
};

export type UserBrief = {
  id: string;
  username: string;
  bio?: string;
  avatar_url?: string;
  followers_count?: number;
};

export type CommentItem = {
  id: string;
  video_id: string;
  user: UserBrief;
  content: string;
  like_count: number;
  created_at: string;
  parent_comment_id?: string | null;
  replies: CommentItem[];
};

export type CommentsData = {
  items: CommentItem[];
  next_cursor?: string;
};

export type UploadTicket = {
  upload_id: string;
  provider: "local" | "s3";
  method: "PUT";
  upload_url: string;
  headers: Record<string, string>;
  object_key: string;
  expires_at: string;
  max_size_bytes: number;
};

export type UploadCompleteData = {
  media_object_id: string;
  object_key: string;
  url: string;
};

export type LoginOrRegisterData = {
  user: {
    id: string;
    username: string;
    email?: string;
    bio?: string;
    avatar_url?: string;
    followers_count?: number;
    following_count?: number;
  };
  tokens: {
    access_token: string;
    access_expires_at: string;
    refresh_token: string;
    refresh_expires_at: string;
  };
};

export type RefreshData = {
  tokens: {
    access_token: string;
    access_expires_at: string;
    refresh_token: string;
    refresh_expires_at: string;
  };
};
