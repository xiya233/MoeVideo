export type AdminOverview = {
  metrics: {
    videos_total: number;
    videos_processing: number;
    transcode_failed: number;
    users_total: number;
    users_active: number;
    uploads_today: number;
    users_today: number;
  };
  recent_failed_jobs: Array<{
    id: string;
    video_id: string;
    last_error: string;
    updated_at: string;
  }>;
  recent_actions: Array<{
    id: string;
    action: string;
    resource_type: string;
    resource_id: string;
    created_at: string;
    actor: {
      id: string;
      username: string;
    };
  }>;
};

export type CursorList<T> = {
  items: T[];
  next_cursor?: string;
};

export type AdminVideoItem = {
  id: string;
  title: string;
  status: string;
  visibility: string;
  published_at: string;
  created_at: string;
  updated_at: string;
  duration_sec: number;
  views_count: number;
  comments_count: number;
  likes_count: number;
  favorites_count: number;
  shares_count: number;
  uploader: {
    id: string;
    username: string;
  };
  category?: {
    id: number;
    slug: string;
    name: string;
  };
};

export type AdminTranscodeJob = {
  id: string;
  video_id: string;
  status: string;
  attempts: number;
  max_attempts: number;
  last_error: string;
  available_at: string;
  locked_at: string;
  started_at: string;
  finished_at: string;
  created_at: string;
  updated_at: string;
};

export type AdminComment = {
  id: string;
  video_id: string;
  video_title: string;
  user_id: string;
  username: string;
  content: string;
  status: string;
  like_count: number;
  reply_count: number;
  created_at: string;
  updated_at: string;
  parent_comment_id?: string | null;
};

export type AdminUser = {
  id: string;
  username: string;
  email: string;
  role: string;
  status: string;
  followers_count: number;
  following_count: number;
  videos_count: number;
  created_at: string;
  updated_at: string;
};

export type AdminAuditLog = {
  id: string;
  action: string;
  resource_type: string;
  resource_id: string;
  payload: unknown;
  ip: string;
  user_agent: string;
  created_at: string;
  actor: {
    id: string;
    username: string;
  };
};

export type FooterLink = {
  label: string;
  url: string;
};

export type FooterLinks = {
  about: FooterLink[];
  support: FooterLink[];
  legal: FooterLink[];
  updates: FooterLink[];
};

export type AdminSiteSettings = {
  site_title: string;
  site_description: string;
  site_logo_media_id?: string | null;
  site_logo_url?: string;
  footer_links: FooterLinks;
  register_enabled: boolean;
  ytdlp_param_mode?: "safe" | "advanced";
  ytdlp_safe?: {
    format?: string;
    extractor_args?: string;
    user_agent?: string;
    referer?: string;
    headers?: Record<string, string>;
    socket_timeout?: number;
  };
  ytdlp_metadata_args_raw?: string;
  ytdlp_download_args_raw?: string;
  updated_by?: string | null;
  updated_at?: string;
};

export type AdminSiteCategory = {
  id: number;
  slug: string;
  name: string;
  sort_order: number;
  is_active: boolean;
};

export type AdminFeaturedBannerVideo = {
  id: string;
  title: string;
  status: string;
  visibility: string;
  cover_url?: string;
};

export type AdminFeaturedBannerItem = {
  position: number;
  video: AdminFeaturedBannerVideo;
};

export type AdminFeaturedBanners = {
  video_ids: string[];
  items: AdminFeaturedBannerItem[];
};
