ALTER TABLE site_settings ADD COLUMN ytdlp_param_mode TEXT NOT NULL DEFAULT 'safe' CHECK (ytdlp_param_mode IN ('safe', 'advanced'));
ALTER TABLE site_settings ADD COLUMN ytdlp_safe_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE site_settings ADD COLUMN ytdlp_metadata_args_raw TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN ytdlp_download_args_raw TEXT NOT NULL DEFAULT '';

ALTER TABLE video_import_jobs ADD COLUMN ytdlp_param_mode TEXT NOT NULL DEFAULT 'safe' CHECK (ytdlp_param_mode IN ('safe', 'advanced'));
ALTER TABLE video_import_jobs ADD COLUMN ytdlp_metadata_args_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE video_import_jobs ADD COLUMN ytdlp_download_args_json TEXT NOT NULL DEFAULT '[]';
