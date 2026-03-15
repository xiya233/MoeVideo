import type {
  Author,
  Category,
  TagItem,
  TagsData,
  CommentItem,
  CommentsData,
  DanmakuData,
  DanmakuItem,
  DanmakuListData,
  ContinueWatchingItem,
  ImportItem,
  ImportJob,
  ImportJobDetailData,
  ImportJobsData,
  HomeData,
  HotRankingsData,
  LoginOrRegisterData,
  TorrentInspectResult,
  UserProfileData,
  UploadCompleteData,
  UploadTicket,
  VideoCard,
  VideoDetail,
  UserBrief,
} from "@/lib/dto";

function str(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function num(value: unknown, fallback = 0): number {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return fallback;
}

function bool(value: unknown, fallback = false): boolean {
  return typeof value === "boolean" ? value : fallback;
}

function arr(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function obj(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
}

export function mapAuthor(value: unknown): Author {
  const src = obj(value);
  return {
    id: str(src.id),
    username: str(src.username, "匿名作者"),
    followers_count: num(src.followers_count),
    avatar_url: str(src.avatar_url) || undefined,
    followed: bool(src.followed),
  };
}

export function mapUserBrief(value: unknown): UserBrief {
  const src = obj(value);
  return {
    id: str(src.id),
    username: str(src.username, "匿名用户"),
    bio: str(src.bio) || undefined,
    avatar_url: str(src.avatar_url) || undefined,
    followers_count: num(src.followers_count),
    following_count: num(src.following_count),
    followed: bool(src.followed),
    profile_public: bool(src.profile_public, true),
    public_videos: bool(src.public_videos, true),
    public_favorites: bool(src.public_favorites),
    public_following: bool(src.public_following),
    public_followers: bool(src.public_followers),
  };
}

export function mapUserProfileData(value: unknown): UserProfileData {
  const src = obj(value);
  return {
    user: mapUserBrief(src.user),
    followed: bool(src.followed),
    profile_accessible: bool(src.profile_accessible, true),
  };
}

export function mapCategory(value: unknown): Category {
  const src = obj(value);
  return {
    id: num(src.id),
    slug: str(src.slug),
    name: str(src.name, "未分类"),
    sort_order: num(src.sort_order),
  };
}

export function mapTagItem(value: unknown): TagItem {
  const src = obj(value);
  return {
    name: str(src.name),
    videos_count: num(src.videos_count),
    use_count: num(src.use_count),
  };
}

export function mapTagsData(value: unknown): TagsData {
  const src = obj(value);
  return {
    items: arr(src.items).map(mapTagItem),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapVideoCard(value: unknown): VideoCard {
  const src = obj(value);
  const categoryID = num(src.category_id);
  return {
    id: str(src.id),
    title: str(src.title, "未命名视频"),
    status: str(src.status) || undefined,
    visibility: str(src.visibility) || undefined,
    category_id: categoryID > 0 ? categoryID : undefined,
    cover_url: str(src.cover_url) || undefined,
    preview_webp_url: str(src.preview_webp_url) || undefined,
    duration_sec: num(src.duration_sec),
    views_count: num(src.views_count),
    comments_count: num(src.comments_count),
    published_at: str(src.published_at),
    category: str(src.category),
    author: mapAuthor(src.author),
  };
}

export function mapHomeData(value: unknown): HomeData {
  const src = obj(value);
  const featuredRaw = src.featured;
  return {
    featured: featuredRaw ? mapVideoCard(featuredRaw) : null,
    featured_items: arr(src.featured_items).map(mapVideoCard),
    hot_rankings: arr(src.hot_rankings).map(mapVideoCard),
    categories: arr(src.categories).map(mapCategory),
    videos: arr(src.videos).map(mapVideoCard),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapHotRankingsData(value: unknown): HotRankingsData {
  const src = obj(value);
  return {
    items: arr(src.items).map(mapVideoCard),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapVideoDetail(value: unknown): VideoDetail {
  const src = obj(value);
  const stats = obj(src.stats);
  const actions = obj(src.viewer_actions);
  const playback = obj(src.playback);
  const variants = arr(playback.variants).map((item) => {
    const raw = obj(item);
    return {
      name: str(raw.name),
      width: num(raw.width),
      height: num(raw.height),
      bandwidth: num(raw.bandwidth),
      url: str(raw.url),
    };
  });
  const statusRaw = str(src.status);
  const status: VideoDetail["status"] =
    statusRaw === "processing" || statusRaw === "failed" || statusRaw === "published"
      ? statusRaw
      : "published";
  const playbackStatusRaw = str(playback.status);
  const playbackStatus: VideoDetail["playback"]["status"] =
    playbackStatusRaw === "processing" || playbackStatusRaw === "failed" || playbackStatusRaw === "ready"
      ? playbackStatusRaw
      : "ready";
  const playbackTypeRaw = str(playback.type);
  const playbackType: VideoDetail["playback"]["type"] =
    playbackTypeRaw === "hls" || playbackTypeRaw === "mp4" ? playbackTypeRaw : "";

  return {
    status,
    video: mapVideoCard(src.video),
    source_url: str(src.source_url),
    viewer_progress_sec: num(src.viewer_progress_sec),
    playback: {
      status: playbackStatus,
      type: playbackType,
      hls_master_url: str(playback.hls_master_url) || undefined,
      mp4_url: str(playback.mp4_url) || undefined,
      vtt_thumbnail_url: str(playback.vtt_thumbnail_url) || undefined,
      variants,
    },
    description: str(src.description),
    tags: arr(src.tags).map((item) => str(item)).filter(Boolean),
    stats: {
      views_count: num(stats.views_count),
      likes_count: num(stats.likes_count),
      favorites_count: num(stats.favorites_count),
      comments_count: num(stats.comments_count),
      shares_count: num(stats.shares_count),
    },
    uploader: mapAuthor(src.uploader),
    viewer_actions: {
      liked: bool(actions.liked),
      favorited: bool(actions.favorited),
      following_uploader: bool(actions.following_uploader),
    },
  };
}

function mapCommentItem(value: unknown): CommentItem {
  const src = obj(value);
  return {
    id: str(src.id),
    video_id: str(src.video_id),
    content: str(src.content),
    like_count: num(src.like_count),
    liked: bool(src.liked),
    created_at: str(src.created_at),
    parent_comment_id: str(src.parent_comment_id) || null,
    user: mapUserBrief(src.user),
    replies: arr(src.replies).map(mapCommentItem),
  };
}

export function mapCommentsData(value: unknown): CommentsData {
  const src = obj(value);
  return {
    items: arr(src.items).map(mapCommentItem),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapUploadTicket(value: unknown): UploadTicket {
  const src = obj(value);
  const headersRaw = obj(src.headers);
  const headers = Object.entries(headersRaw).reduce<Record<string, string>>((acc, [key, val]) => {
    acc[key] = str(val);
    return acc;
  }, {});

  return {
    upload_id: str(src.upload_id),
    provider: str(src.provider) === "s3" ? "s3" : "local",
    method: "PUT",
    upload_url: str(src.upload_url),
    headers,
    object_key: str(src.object_key),
    expires_at: str(src.expires_at),
    max_size_bytes: num(src.max_size_bytes),
  };
}

export function mapUploadCompleteData(value: unknown): UploadCompleteData {
  const src = obj(value);
  return {
    media_object_id: str(src.media_object_id),
    object_key: str(src.object_key),
    url: str(src.url),
  };
}

export function mapDanmakuItem(value: unknown): DanmakuItem {
  const src = obj(value);
  const modeRaw = num(src.mode);
  const mode: DanmakuItem["mode"] = modeRaw === 1 || modeRaw === 2 ? modeRaw : 0;
  return {
    id: str(src.id),
    video_id: str(src.video_id),
    user_id: str(src.user_id),
    content: str(src.content),
    time_sec: num(src.time_sec),
    mode,
    color: str(src.color, "#FFFFFF"),
    created_at: str(src.created_at),
  };
}

export function mapDanmakuData(value: unknown): DanmakuData {
  const src = obj(value);
  return {
    items: arr(src.items).map(mapDanmakuItem),
  };
}

export function mapDanmakuListData(value: unknown): DanmakuListData {
  const src = obj(value);
  return {
    items: arr(src.items).map(mapDanmakuItem),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapLoginRegisterData(value: unknown): LoginOrRegisterData {
  const src = obj(value);
  const user = obj(src.user);
  return {
    user: {
      id: str(user.id),
      username: str(user.username),
      email: str(user.email) || undefined,
      role: str(user.role) || undefined,
      bio: str(user.bio) || undefined,
      avatar_url: str(user.avatar_url) || undefined,
      followers_count: num(user.followers_count),
      following_count: num(user.following_count),
      profile_public: bool(user.profile_public, true),
      public_videos: bool(user.public_videos, true),
      public_favorites: bool(user.public_favorites),
      public_following: bool(user.public_following),
      public_followers: bool(user.public_followers),
    },
  };
}

export function mapContinueWatchingItem(value: unknown): ContinueWatchingItem {
  const src = obj(value);
  return {
    video: mapVideoCard(src.video),
    position_sec: num(src.position_sec),
    duration_sec: num(src.duration_sec),
    progress_percent: num(src.progress_percent),
    updated_at: str(src.updated_at),
  };
}

export function mapImportJob(value: unknown): ImportJob {
  const src = obj(value);
  const sourceTypeRaw = str(src.source_type);
  const sourceType: ImportJob["source_type"] = sourceTypeRaw === "url" ? "url" : "torrent";
  const statusRaw = str(src.status);
  const status: ImportJob["status"] =
    statusRaw === "queued" ||
    statusRaw === "downloading" ||
    statusRaw === "succeeded" ||
    statusRaw === "partial" ||
    statusRaw === "failed"
      ? statusRaw
      : "draft";
  const visibilityRaw = str(src.visibility);
  const visibility: ImportJob["visibility"] =
    visibilityRaw === "private" || visibilityRaw === "unlisted" ? visibilityRaw : "public";
  const ytdlpModeRaw = str(src.ytdlp_param_mode);
  const ytdlpMode: ImportJob["ytdlp_param_mode"] = ytdlpModeRaw === "advanced" ? "advanced" : "safe";

  return {
    id: str(src.id),
    source_type: sourceType,
    source_filename: str(src.source_filename) || undefined,
    info_hash: str(src.info_hash) || undefined,
    source_url: str(src.source_url) || undefined,
    resolved_media_url: str(src.resolved_media_url) || undefined,
    resolver_name: str(src.resolver_name) || undefined,
    ytdlp_param_mode: ytdlpMode,
    custom_title: str(src.custom_title) || undefined,
    custom_title_prefix: str(src.custom_title_prefix) || undefined,
    custom_description: str(src.custom_description) || undefined,
    status,
    draft_expired: bool(src.draft_expired),
    category_id: num(src.category_id) || undefined,
    tags: arr(src.tags).map((item) => str(item)).filter(Boolean),
    visibility,
    total_files: num(src.total_files),
    selected_files: num(src.selected_files),
    completed_files: num(src.completed_files),
    failed_files: num(src.failed_files),
    progress: num(src.progress),
    downloaded_bytes: num(src.downloaded_bytes),
    uploaded_bytes: num(src.uploaded_bytes),
    download_speed_bps: num(src.download_speed_bps),
    upload_speed_bps: num(src.upload_speed_bps),
    transfer_updated_at: str(src.transfer_updated_at) || undefined,
    available_at: str(src.available_at) || undefined,
    started_at: str(src.started_at) || undefined,
    finished_at: str(src.finished_at) || undefined,
    expires_at: str(src.expires_at) || undefined,
    error_message: str(src.error_message) || undefined,
    created_at: str(src.created_at),
    updated_at: str(src.updated_at),
  };
}

export function mapImportItem(value: unknown): ImportItem {
  const src = obj(value);
  const statusRaw = str(src.status);
  const status: ImportItem["status"] =
    statusRaw === "downloading" ||
    statusRaw === "completed" ||
    statusRaw === "failed" ||
    statusRaw === "skipped"
      ? statusRaw
      : "pending";

  return {
    id: str(src.id),
    file_index: num(src.file_index),
    file_path: str(src.file_path),
    file_size_bytes: num(src.file_size_bytes),
    selected: bool(src.selected),
    status,
    error_message: str(src.error_message) || undefined,
    media_object_id: str(src.media_object_id) || undefined,
    video_id: str(src.video_id) || undefined,
    created_at: str(src.created_at) || undefined,
    updated_at: str(src.updated_at) || undefined,
  };
}

export function mapTorrentInspectResult(value: unknown): TorrentInspectResult {
  const src = obj(value);
  return {
    job: mapImportJob(src.job),
    items: arr(src.items).map(mapImportItem),
  };
}

export function mapImportJobsData(value: unknown): ImportJobsData {
  const src = obj(value);
  return {
    items: arr(src.items).map(mapImportJob),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapImportJobDetailData(value: unknown): ImportJobDetailData {
  const src = obj(value);
  return {
    job: mapImportJob(src.job),
    items: arr(src.items).map(mapImportItem),
    created_video_ids: arr(src.created_video_ids)
      .map((item) => str(item))
      .filter(Boolean),
  };
}
