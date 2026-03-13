CREATE TABLE IF NOT EXISTS video_import_jobs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    source_type TEXT NOT NULL CHECK (source_type IN ('torrent')),
    source_filename TEXT NOT NULL,
    info_hash TEXT NOT NULL,
    torrent_data BLOB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'queued', 'downloading', 'succeeded', 'partial', 'failed')),
    category_id INTEGER,
    tags_json TEXT NOT NULL DEFAULT '[]',
    visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private', 'unlisted')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    total_files INTEGER NOT NULL DEFAULT 0,
    selected_files INTEGER NOT NULL DEFAULT 0,
    completed_files INTEGER NOT NULL DEFAULT 0,
    failed_files INTEGER NOT NULL DEFAULT 0,
    progress REAL NOT NULL DEFAULT 0,
    available_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    expires_at TEXT NOT NULL,
    error_message TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS video_import_items (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    file_index INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    file_size_bytes INTEGER NOT NULL DEFAULT 0,
    selected INTEGER NOT NULL DEFAULT 0 CHECK (selected IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('pending', 'downloading', 'completed', 'failed', 'skipped')),
    error_message TEXT,
    media_object_id TEXT,
    video_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(job_id, file_index),
    FOREIGN KEY (job_id) REFERENCES video_import_jobs(id) ON DELETE CASCADE,
    FOREIGN KEY (media_object_id) REFERENCES media_objects(id),
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

CREATE INDEX IF NOT EXISTS idx_video_import_jobs_user_created
ON video_import_jobs(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_video_import_jobs_status_available
ON video_import_jobs(status, available_at);

CREATE INDEX IF NOT EXISTS idx_video_import_items_job_file
ON video_import_items(job_id, file_index);
