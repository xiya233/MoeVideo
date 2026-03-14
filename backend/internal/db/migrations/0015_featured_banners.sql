CREATE TABLE IF NOT EXISTS site_featured_banners (
    position INTEGER PRIMARY KEY,
    video_id TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (position >= 1 AND position <= 5),
    FOREIGN KEY (video_id) REFERENCES videos(id)
);

