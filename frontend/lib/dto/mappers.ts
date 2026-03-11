import type {
  Author,
  Category,
  CommentItem,
  CommentsData,
  HomeData,
  LoginOrRegisterData,
  UploadCompleteData,
  UploadTicket,
  VideoCard,
  VideoDetail,
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

export function mapCategory(value: unknown): Category {
  const src = obj(value);
  return {
    id: num(src.id),
    slug: str(src.slug),
    name: str(src.name, "未分类"),
    sort_order: num(src.sort_order),
  };
}

export function mapVideoCard(value: unknown): VideoCard {
  const src = obj(value);
  return {
    id: str(src.id),
    title: str(src.title, "未命名视频"),
    cover_url: str(src.cover_url) || undefined,
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
    hot_rankings: arr(src.hot_rankings).map(mapVideoCard),
    categories: arr(src.categories).map(mapCategory),
    videos: arr(src.videos).map(mapVideoCard),
    next_cursor: str(src.next_cursor) || undefined,
  };
}

export function mapVideoDetail(value: unknown): VideoDetail {
  const src = obj(value);
  const stats = obj(src.stats);
  const actions = obj(src.viewer_actions);
  return {
    video: mapVideoCard(src.video),
    source_url: str(src.source_url),
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
  const user = obj(src.user);
  return {
    id: str(src.id),
    video_id: str(src.video_id),
    content: str(src.content),
    like_count: num(src.like_count),
    created_at: str(src.created_at),
    parent_comment_id: str(src.parent_comment_id) || null,
    user: {
      id: str(user.id),
      username: str(user.username, "匿名用户"),
      avatar_url: str(user.avatar_url) || undefined,
      bio: str(user.bio) || undefined,
      followers_count: num(user.followers_count),
    },
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

export function mapLoginRegisterData(value: unknown): LoginOrRegisterData {
  const src = obj(value);
  const user = obj(src.user);
  const tokens = obj(src.tokens);
  return {
    user: {
      id: str(user.id),
      username: str(user.username),
      email: str(user.email) || undefined,
      bio: str(user.bio) || undefined,
      avatar_url: str(user.avatar_url) || undefined,
      followers_count: num(user.followers_count),
      following_count: num(user.following_count),
    },
    tokens: {
      access_token: str(tokens.access_token),
      access_expires_at: str(tokens.access_expires_at),
      refresh_token: str(tokens.refresh_token),
      refresh_expires_at: str(tokens.refresh_expires_at),
    },
  };
}
