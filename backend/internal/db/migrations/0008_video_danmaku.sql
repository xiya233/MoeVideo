CREATE TABLE IF NOT EXISTS video_danmaku (
    id TEXT PRIMARY KEY,
    video_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    content TEXT NOT NULL,
    time_sec REAL NOT NULL CHECK (time_sec >= 0),
    mode INTEGER NOT NULL DEFAULT 0 CHECK (mode IN (0, 1, 2)),
    color TEXT NOT NULL DEFAULT '#FFFFFF',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
    created_at TEXT NOT NULL,
    FOREIGN KEY (video_id) REFERENCES videos(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_video_danmaku_video_time
ON video_danmaku(video_id, time_sec);

CREATE INDEX IF NOT EXISTS idx_video_danmaku_video_created
ON video_danmaku(video_id, created_at DESC);
