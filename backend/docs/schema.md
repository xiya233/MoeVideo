# SQLite Schema

Migration files:

- `internal/db/migrations/0001_init.sql`
- `internal/db/migrations/0002_hls_transcode.sql`
- `internal/db/migrations/0003_playback_preview.sql`

## Runtime pragmas

- `PRAGMA journal_mode=WAL`
- `PRAGMA synchronous=NORMAL`
- `PRAGMA foreign_keys=ON`
- `PRAGMA busy_timeout=5000`

## Core tables

- `users`
- `auth_refresh_tokens`
- `follows`
- `categories`
- `tags`
- `media_objects`
- `upload_sessions`
- `videos`
- `video_tags`
- `video_actions`
- `comments`
- `comment_likes`
- `video_view_events`
- `video_transcode_jobs`
- `video_hls_assets`
- `user_video_progress`

`videos` table additions:

- `preview_media_id` (nullable FK to `media_objects`)

## Constraints

- one-level replies enforced by trigger: `trg_comments_only_one_level`
- follow self prevented by `CHECK(follower_id != followee_id)`
- upload max size capped by DB check: `max_size_bytes <= 2147483648`

## Hot paths indexes

- `videos(status, published_at DESC)`
- `videos(category_id, published_at DESC)`
- `videos(hot_score DESC)`
- `comments(video_id, parent_comment_id, created_at DESC)`
- `video_actions(video_id, action_type)`
- `follows(followee_id)`
- `video_transcode_jobs(status, available_at)`
- `video_hls_assets(video_id)`
- `user_video_progress(updated_at DESC)`
- `videos(preview_media_id)`
