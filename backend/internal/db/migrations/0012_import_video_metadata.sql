ALTER TABLE video_import_jobs ADD COLUMN custom_title TEXT;
ALTER TABLE video_import_jobs ADD COLUMN custom_title_prefix TEXT;
ALTER TABLE video_import_jobs ADD COLUMN custom_description TEXT NOT NULL DEFAULT '';

