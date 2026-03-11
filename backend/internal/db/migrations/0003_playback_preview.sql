CREATE TABLE IF NOT EXISTS user_video_progress (
    user_id TEXT NOT NULL,
    video_id TEXT NOT NULL,
    position_sec INTEGER NOT NULL DEFAULT 0,
    duration_sec INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, video_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

ALTER TABLE videos ADD COLUMN preview_media_id TEXT REFERENCES media_objects(id);

CREATE INDEX IF NOT EXISTS idx_user_video_progress_updated
ON user_video_progress(updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_videos_preview_media_id
ON videos(preview_media_id);
