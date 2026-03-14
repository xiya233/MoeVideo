# MoeVideo API v1

Base URL: `/api/v1`

## Envelope

All responses follow:

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

- `code=0` means success.
- non-zero values map to HTTP status on failure.

## Auth

- `POST /auth/register`
- `POST /auth/login`
- `POST /auth/refresh`
- `POST /auth/logout`

Auth header format:

```http
Authorization: Bearer <access_jwt>
```

## User

- `GET /users/me`
- `GET /users/{userId}`
- `PUT /users/{userId}/follow`
- `GET /users/me/videos`

## Discovery

- `GET /home`
- `GET /categories`
- `GET /tags`
- `GET /rankings/hot`
- `GET /videos`

Query params:

- `cursor`
- `limit` (default 20, max 50)
- `q`
- `category`
- `sort` (`latest|hot`)
- `tag` (按标签精确过滤)

## Video Playback

- `GET /videos/{videoId}`
- `GET /videos/{videoId}/recommendations`
- `POST /videos/{videoId}/view`
- `PUT /videos/{videoId}/like`
- `PUT /videos/{videoId}/favorite`
- `POST /videos/{videoId}/share`
- `PUT /videos/{videoId}/progress` (auth required)

### `GET /videos/{videoId}` additions

- top-level `status`: `processing | published | failed`
- optional `viewer_progress_sec` (only when logged in)
- `video` adds optional `category_id` (for category filter jump)
- `playback`:
  - `status`: `processing | ready | failed`
  - `type`: `hls | mp4 | ""`
  - optional `hls_master_url`
  - optional `mp4_url`
  - optional `vtt_thumbnail_url`
  - optional `variants[]`

Visibility rules:

- public users can only access `published + public`
- uploader can access `processing/failed` details for polling

### `GET /videos/{videoId}/recommendations` query

- `limit`
- `random=1`（启用随机推荐）
- `exclude_ids=id1,id2,...`（可选，排除指定视频）

## Comments (one-level reply)

- `GET /videos/{videoId}/comments`
- `POST /videos/{videoId}/comments`
- `PUT /comments/{commentId}/like`
- `DELETE /comments/{commentId}`

## Upload + Publish

- `POST /uploads/presign`
- `PUT /uploads/local/{uploadToken}` (local only)
- `POST /uploads/{uploadId}/complete`
- `POST /videos`
- `DELETE /videos/{videoId}`

`POST /videos` request constraints:

- `category_id` is required and must reference an active category.

### Upload limits

- max size configurable by `MAX_UPLOAD_MB` (default 2048 MB)
- supported video MIME:
  - `video/mp4`, `video/quicktime`, `video/x-msvideo`, `video/webm`
  - `video/x-matroska`, `application/x-matroska`, `video/x-flv`
  - `video/mpeg`, `video/3gpp`, `video/x-m4v`, `video/mp2t`
- supported video extensions (MIME fallback): `mp4,mov,avi,webm,mkv,flv,mpeg,mpg,3gp,m4v,ts`
- supported cover MIME: `image/jpeg`, `image/png`, `image/webp`

### `POST /videos` additions

- response payload now includes `{ id, status }`
- new videos are created with `status=processing`
- backend worker transcodes HLS and then promotes to `published`
- if user did not upload cover, worker auto-generates cover (`cover.jpg`)
- worker also generates hover preview (`preview.webp`) and returns it via `VideoCard.preview_webp_url`

### `PUT /videos/{videoId}/progress`

Request body:

```json
{
  "position_sec": 120,
  "duration_sec": 600,
  "completed": false
}
```

Behavior:

- upsert progress for current user
- when `completed=true` or near video end, progress is cleared
- response: `{ "saved": true, "position_sec": 120 }`

## Import

- `POST /imports/torrent/inspect`
- `POST /imports/torrent/start`
- `POST /imports/url/start`
- `GET /imports`
- `GET /imports/{jobId}`

Request constraints:

- `POST /imports/torrent/start` requires `category_id` and selected files.
- `POST /imports/url/start` requires `category_id`.

## Admin (admin only)

All admin routes require:

- valid access token
- current user `role=admin` and `status=active`

Endpoints:

- `GET /admin/overview`
- `GET /admin/videos`
- `GET /admin/videos/{id}`
- `POST /admin/videos/{id}/actions` (`publish|hide|soft_delete|restore|retry_transcode`)
- `GET /admin/transcode-jobs`
- `POST /admin/transcode-jobs/{jobId}/retry`
- `GET /admin/comments`
- `POST /admin/comments/actions` (`delete|restore`)
- `GET /admin/users`
- `PATCH /admin/users/{id}` (`status`, `role`)
- `GET /admin/audit-logs`

All admin write operations are persisted into `admin_audit_logs`.
