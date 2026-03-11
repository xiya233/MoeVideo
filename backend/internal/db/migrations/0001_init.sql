CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    avatar_media_id TEXT,
    bio TEXT NOT NULL DEFAULT '',
    followers_count INTEGER NOT NULL DEFAULT 0,
    following_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (avatar_media_id) REFERENCES media_objects(id)
);

CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS follows (
    follower_id TEXT NOT NULL,
    followee_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id != followee_id),
    FOREIGN KEY (follower_id) REFERENCES users(id),
    FOREIGN KEY (followee_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1))
);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    use_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_objects (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL CHECK (provider IN ('local', 's3')),
    bucket TEXT,
    object_key TEXT NOT NULL UNIQUE,
    original_filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    checksum_sha256 TEXT,
    duration_sec INTEGER NOT NULL DEFAULT 0,
    width INTEGER,
    height INTEGER,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS upload_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('video', 'cover')),
    provider TEXT NOT NULL CHECK (provider IN ('local', 's3')),
    object_key TEXT NOT NULL UNIQUE,
    content_type TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    file_size_bytes INTEGER NOT NULL DEFAULT 0,
    max_size_bytes INTEGER NOT NULL CHECK (max_size_bytes <= 2147483648),
    status TEXT NOT NULL CHECK (status IN ('initiated', 'uploaded', 'completed', 'expired', 'failed')),
    upload_token TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    completed_at TEXT,
    media_object_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (media_object_id) REFERENCES media_objects(id)
);

CREATE TABLE IF NOT EXISTS videos (
    id TEXT PRIMARY KEY,
    uploader_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category_id INTEGER,
    cover_media_id TEXT,
    source_media_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('processing', 'published', 'failed', 'deleted')),
    visibility TEXT NOT NULL CHECK (visibility IN ('public', 'private', 'unlisted')),
    duration_sec INTEGER NOT NULL DEFAULT 0,
    published_at TEXT,
    views_count INTEGER NOT NULL DEFAULT 0,
    likes_count INTEGER NOT NULL DEFAULT 0,
    favorites_count INTEGER NOT NULL DEFAULT 0,
    comments_count INTEGER NOT NULL DEFAULT 0,
    shares_count INTEGER NOT NULL DEFAULT 0,
    hot_score REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (uploader_id) REFERENCES users(id),
    FOREIGN KEY (category_id) REFERENCES categories(id),
    FOREIGN KEY (cover_media_id) REFERENCES media_objects(id),
    FOREIGN KEY (source_media_id) REFERENCES media_objects(id)
);

CREATE TABLE IF NOT EXISTS video_tags (
    video_id TEXT NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (video_id, tag_id),
    FOREIGN KEY (video_id) REFERENCES videos(id),
    FOREIGN KEY (tag_id) REFERENCES tags(id)
);

CREATE TABLE IF NOT EXISTS video_actions (
    user_id TEXT NOT NULL,
    video_id TEXT NOT NULL,
    action_type TEXT NOT NULL CHECK (action_type IN ('like', 'favorite')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (user_id, video_id, action_type),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    video_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    parent_comment_id TEXT,
    content TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'deleted')),
    like_count INTEGER NOT NULL DEFAULT 0,
    reply_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (video_id) REFERENCES videos(id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (parent_comment_id) REFERENCES comments(id)
);

CREATE TABLE IF NOT EXISTS comment_likes (
    user_id TEXT NOT NULL,
    comment_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (user_id, comment_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (comment_id) REFERENCES comments(id)
);

CREATE TABLE IF NOT EXISTS video_view_events (
    id TEXT PRIMARY KEY,
    video_id TEXT NOT NULL,
    viewer_key TEXT NOT NULL,
    viewed_minute TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(video_id, viewer_key, viewed_minute),
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_user_id ON auth_refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_expires_at ON auth_refresh_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_follows_followee_id ON follows(followee_id);
CREATE INDEX IF NOT EXISTS idx_videos_status_published_at ON videos(status, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_category_published_at ON videos(category_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_hot_score ON videos(hot_score DESC);
CREATE INDEX IF NOT EXISTS idx_video_actions_video_action_type ON video_actions(video_id, action_type);
CREATE INDEX IF NOT EXISTS idx_comments_video_parent_created ON comments(video_id, parent_comment_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_upload_sessions_user_created ON upload_sessions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_video_view_events_video_created ON video_view_events(video_id, created_at DESC);

CREATE TRIGGER IF NOT EXISTS trg_comments_only_one_level
BEFORE INSERT ON comments
WHEN NEW.parent_comment_id IS NOT NULL
BEGIN
    SELECT CASE
      WHEN EXISTS (
          SELECT 1
          FROM comments parent
          WHERE parent.id = NEW.parent_comment_id
            AND parent.parent_comment_id IS NOT NULL
      ) THEN RAISE(ABORT, 'nested replies are not allowed')
    END;
END;
