ALTER TABLE videos ADD COLUMN is_live INTEGER NOT NULL DEFAULT 0;
ALTER TABLE videos ADD COLUMN live_hls_url TEXT;
ALTER TABLE videos ADD COLUMN live_started_at TEXT;

CREATE INDEX IF NOT EXISTS idx_videos_live_published
ON videos(is_live DESC, published_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS live_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    video_id TEXT NOT NULL,
    stream_key TEXT NOT NULL UNIQUE,
    app_name TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category_id INTEGER,
    tags_json TEXT NOT NULL DEFAULT '[]',
    visibility TEXT NOT NULL CHECK (visibility IN ('public', 'private', 'unlisted')),
    status TEXT NOT NULL CHECK (status IN ('waiting', 'live', 'ended', 'failed')),
    publish_url TEXT NOT NULL,
    playback_url TEXT NOT NULL,
    record_file_path TEXT,
    started_at TEXT,
    ended_at TEXT,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (video_id) REFERENCES videos(id) ON DELETE CASCADE,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

CREATE INDEX IF NOT EXISTS idx_live_sessions_user_status_created
ON live_sessions(user_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_live_sessions_stream_app
ON live_sessions(stream_key, app_name);
