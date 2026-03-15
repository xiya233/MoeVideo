ALTER TABLE video_import_jobs ADD COLUMN downloaded_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_import_jobs ADD COLUMN uploaded_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE video_import_jobs ADD COLUMN download_speed_bps REAL NOT NULL DEFAULT 0;
ALTER TABLE video_import_jobs ADD COLUMN upload_speed_bps REAL NOT NULL DEFAULT 0;
ALTER TABLE video_import_jobs ADD COLUMN transfer_updated_at TEXT;
