# Live Streaming (SRS) Integration

This project uses **SRS** for live ingest/playback in phase 1:

- OBS pushes RTMP to SRS
- SRS serves HLS playback
- SRS callback notifies MoeVideo (`on_publish` / `on_unpublish` / `on_record`)
- MoeVideo turns record files into replay videos via existing transcode pipeline

## Required Backend Env

Add these to `backend/.env`:

```env
LIVE_ENABLED=true
LIVE_APP_NAME=live
LIVE_RTMP_SERVER_URL=rtmp://127.0.0.1
LIVE_PLAYBACK_BASE_URL=https://your-domain/live-hls
LIVE_CALLBACK_SECRET=replace-with-strong-secret
LIVE_RECORD_DIR=./data/live-recordings
```

## SRS Callback Endpoint

Backend callback endpoint:

```text
POST /api/v1/live/srs/callback
```

Recommended callback URL:

```text
https://your-domain/api/v1/live/srs/callback?token=${LIVE_CALLBACK_SECRET}
```

## Minimal SRS vhost Example

> Adjust paths/domains for your host.

```conf
vhost __defaultVhost__ {
    rtc {
        enabled off;
    }

    http_remux {
        enabled on;
        mount [vhost]/[app]/[stream].m3u8;
        hstrs on;
    }

    dvr {
        enabled on;
        dvr_apply all;
        dvr_path /opt/srs/records/[app]/[stream]/[timestamp].flv;
        dvr_plan session;
        dvr_wait_keyframe on;
    }

    http_hooks {
        enabled on;
        on_publish   https://your-domain/api/v1/live/srs/callback?token=REPLACE_ME;
        on_unpublish https://your-domain/api/v1/live/srs/callback?token=REPLACE_ME;
    }
}
```

If you also emit custom record callbacks, point them to the same endpoint.

## Notes

- Live sessions are created from `/live/studio`.
- Homepage/category latest feed prioritizes `is_live=true`.
- Live cards show a `LIVE` badge on cover.
- Manual end is available from studio page.
