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
  status?: string;
  visibility?: string;
  cover_url?: string;
  preview_webp_url?: string;
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
  viewer_progress_sec?: number;
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
  following_count?: number;
  followed?: boolean;
  profile_public?: boolean;
  public_videos?: boolean;
  public_favorites?: boolean;
  public_following?: boolean;
  public_followers?: boolean;
};

export type UserProfileData = {
  user: UserBrief;
  followed: boolean;
  profile_accessible: boolean;
};

export type CommentItem = {
  id: string;
  video_id: string;
  user: UserBrief;
  content: string;
  like_count: number;
  liked: boolean;
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

export type SaveProgressData = {
  saved: boolean;
  position_sec: number;
};

export type DanmakuItem = {
  id: string;
  video_id: string;
  user_id: string;
  content: string;
  time_sec: number;
  mode: 0 | 1 | 2;
  color: string;
  created_at: string;
};

export type DanmakuData = {
  items: DanmakuItem[];
};

export type DanmakuListData = {
  items: DanmakuItem[];
  next_cursor?: string;
};

export type ContinueWatchingItem = {
  video: VideoCard;
  position_sec: number;
  duration_sec: number;
  progress_percent: number;
  updated_at: string;
};

export type LoginOrRegisterData = {
  user: {
    id: string;
    username: string;
    email?: string;
    role?: string;
    bio?: string;
    avatar_url?: string;
    followers_count?: number;
    following_count?: number;
    profile_public?: boolean;
    public_videos?: boolean;
    public_favorites?: boolean;
    public_following?: boolean;
    public_followers?: boolean;
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
