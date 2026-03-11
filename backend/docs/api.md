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
- `GET /rankings/hot`
- `GET /videos`

Query params:

- `cursor`
- `limit` (default 20, max 50)
- `q`
- `category`
- `sort` (`latest|hot`)

## Video Playback

- `GET /videos/{videoId}`
- `GET /videos/{videoId}/recommendations`
- `POST /videos/{videoId}/view`
- `PUT /videos/{videoId}/like`
- `PUT /videos/{videoId}/favorite`
- `POST /videos/{videoId}/share`

### `GET /videos/{videoId}` additions

- top-level `status`: `processing | published | failed`
- `playback`:
  - `status`: `processing | ready | failed`
  - `type`: `hls | mp4 | ""`
  - optional `hls_master_url`
  - optional `mp4_url`
  - optional `variants[]`

Visibility rules:

- public users can only access `published + public`
- uploader can access `processing/failed` details for polling

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

### Upload limits

- max size configurable by `MAX_UPLOAD_MB` (default 2048 MB)
- supported video MIME: `video/mp4`, `video/quicktime`, `video/x-msvideo`, `video/webm`
- supported cover MIME: `image/jpeg`, `image/png`, `image/webp`

### `POST /videos` additions

- response payload now includes `{ id, status }`
- new videos are created with `status=processing`
- backend worker transcodes HLS and then promotes to `published`
