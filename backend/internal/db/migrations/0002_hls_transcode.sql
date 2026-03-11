CREATE TABLE IF NOT EXISTS video_transcode_jobs (
    id TEXT PRIMARY KEY,
    video_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('queued', 'processing', 'succeeded', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    last_error TEXT,
    available_at TEXT NOT NULL,
    locked_at TEXT,
    started_at TEXT,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

CREATE TABLE IF NOT EXISTS video_hls_assets (
    video_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL CHECK (provider IN ('local', 's3')),
    bucket TEXT,
    master_object_key TEXT NOT NULL UNIQUE,
    variants_json TEXT NOT NULL,
    segment_seconds INTEGER NOT NULL DEFAULT 4,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

CREATE INDEX IF NOT EXISTS idx_video_transcode_jobs_status_available
ON video_transcode_jobs(status, available_at);

CREATE INDEX IF NOT EXISTS idx_video_hls_assets_video_id
ON video_hls_assets(video_id);
