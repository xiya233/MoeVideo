# 直播（SRS）集成与部署指南

本文是 MoeVideo 一期直播能力的落地文档，包含：

1. SRS 如何部署  
2. MoeVideo 后端 `LIVE_*` 如何配置  
3. OBS 如何推流  
4. Nginx 如何反代直播播放  

当前直播链路：

- OBS -> SRS（RTMP 入站）
- SRS -> HLS（直播播放）
- SRS 回调 MoeVideo（`on_publish` / `on_unpublish` / `on_dvr`）
- MoeVideo 根据录制文件自动转回放并发布

> 说明：项目根目录 `docker-compose.yml` 已内置 `srs` 服务。  
> 现在可以直接用 `docker compose --env-file .env.docker up -d --build` 一起启动 `frontend + backend + redis + srs`。

---

## 1. 端口与路径规划（建议）

建议先统一以下约定：

- SRS RTMP 端口：`1935`
- SRS HTTP-HLS 端口：`8081`（宿主机）
- MoeVideo Backend：`8080`
- 直播录制目录（宿主机）：`./data/srs/records`

> 如果你是 Docker Compose 部署 MoeVideo，建议让 backend 和 SRS 共享同一份宿主机录制目录。

---

## 2. 推荐部署方式：Docker 部署 SRS

如果你已经使用本项目的 `docker-compose.yml`，可跳过本节的 `docker run` 方式，直接使用 compose 内置 SRS。

### 2.1 创建目录

在项目根目录执行：

```bash
mkdir -p data/srs/conf data/srs/records
```

### 2.2 写入 SRS 配置

创建文件 `data/srs/conf/srs.conf`：

```conf
listen              1935;
max_connections     1000;
daemon              off;
srs_log_tank        console;

http_server {
    enabled on;
    listen 8080;
    # 必须与 hls_path 对齐，否则会出现 m3u8 文件已生成但 HTTP 访问 404
    dir /data/live-recordings;
}

http_api {
    enabled on;
    listen 1985;
}

vhost __defaultVhost__ {
    # RTMP -> HLS
    hls {
        enabled on;
        hls_path /data/live-recordings;
        hls_fragment 6;
        hls_window 60;
    }

    # HTTP-FLV/HLS remux（播放器访问）
    http_remux {
        enabled on;
        # 最终播放地址将是：/live/<app>/<stream>.m3u8
        mount [app]/[stream].m3u8;
    }

    # 录制文件（用于回放自动发布）
    dvr {
        enabled on;
        dvr_apply all;
        dvr_path /data/live-recordings/[app]/[stream]/[timestamp].flv;
        dvr_plan session;
        dvr_wait_keyframe on;
    }

    # 回调到 MoeVideo
    http_hooks {
        enabled on;
        on_publish   https://your-domain/api/v1/live/srs/callback?token=REPLACE_ME;
        on_unpublish https://your-domain/api/v1/live/srs/callback?token=REPLACE_ME;
        on_dvr       https://your-domain/api/v1/live/srs/callback?token=REPLACE_ME;
    }
}
```

将上面 `REPLACE_ME` 改成你自己的回调密钥（与后端 `LIVE_CALLBACK_SECRET` 一致）。

### 2.3 启动 SRS 容器

```bash
docker run -d \
  --name moevideo-srs \
  --restart unless-stopped \
  -p 1935:1935 \
  -p 8081:8080 \
  -v "$(pwd)/data/srs/conf/srs.conf:/usr/local/srs/conf/srs.conf:ro" \
  -v "$(pwd)/data/srs/records:/data/live-recordings" \
  ossrs/srs:5 \
  ./objs/srs -c conf/srs.conf
```

### 2.4 验证 SRS 是否启动

```bash
docker logs -f moevideo-srs
```

看到 `SRS/5` 正常启动日志即可。

---

## 3. MoeVideo 后端配置（`backend/.env`）

最关键是这 6 项：

```env
LIVE_ENABLED=true
LIVE_APP_NAME=live
LIVE_RTMP_SERVER_URL=rtmp://your-domain
LIVE_PLAYBACK_BASE_URL=https://your-domain/live
LIVE_CALLBACK_SECRET=replace-with-strong-secret
LIVE_RECORD_DIR=./data/srs/records
```

说明：

- `LIVE_RTMP_SERVER_URL` + `LIVE_APP_NAME` 会组成开播中心里给 OBS 的推流地址。  
  例如：`rtmp://your-domain/live`
- `LIVE_PLAYBACK_BASE_URL` 会拼接 `/<stream_key>.m3u8`，  
  所以最终播放地址是：`https://your-domain/live/<stream_key>.m3u8`
- `LIVE_RECORD_DIR` 必须指向 SRS 实际落盘录制目录（后端要能读到）

> 如果 backend 在容器里运行，请把宿主机 `./data/srs/records` bind mount 到 backend 容器里，并把 `LIVE_RECORD_DIR` 改成容器内路径（例如 `/data/live-recordings`）。

---

## 4. 宿主机 Nginx 反向代理（直播播放）

如果你已经有 MoeVideo 的 Nginx 站点配置，补充以下 `location`：

```nginx
# MoeVideo API
location /api/ {
    proxy_pass http://127.0.0.1:8080/api/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}

# SRS HLS 播放（对外暴露 /live）
location /live/ {
    proxy_pass http://127.0.0.1:8081/live/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    add_header Cache-Control no-cache;
}
```

生效命令：

```bash
sudo nginx -t
sudo systemctl reload nginx
```

---

## 5. OBS 推流配置

在站点打开：`/live/studio`

1. 创建直播会话
2. 复制页面里的 `推流地址` 和 `串流密钥`
3. OBS -> 设置 -> 推流：
   - 服务：自定义
   - 服务器：`推流地址`
   - 串流密钥：`串流密钥`
4. 点击 OBS「开始推流」

推流后：

- 首页与分类流会出现带 `LIVE` 徽标的视频卡
- 直播中视频会置顶（latest 排序下）

---

## 6. 停播与自动回放

停播有两种方式：

1. OBS 停止推流  
2. 在 `/live/studio` 点击「结束直播」

停播后：

- SRS 触发 `on_unpublish`，录制完成后再触发 `on_dvr`
- MoeVideo 读取录制文件
- 自动进入已有转码发布链路
- 处理完成后成为普通点播视频（分类/标签/可见性继承开播时设置）

---

## 7. 常见排查

### 7.1 开播后首页没出现 LIVE

检查：

- `docker logs moevideo-srs` 是否有 `on_publish`
- backend 日志是否收到 `/api/v1/live/srs/callback`
- `LIVE_CALLBACK_SECRET` 和 SRS 回调 token 是否一致

### 7.2 能推流但播放 404

检查：

- `LIVE_PLAYBACK_BASE_URL` 是否与 SRS `mount` 规则一致
- Nginx 是否正确代理 `/live/` -> `127.0.0.1:8081/live/`
- SRS 配置里 `http_server.dir` 是否与 `hls_path` 一致（建议都为 `/data/live-recordings`）
- 在宿主机直连 SRS 验证：`curl -I http://127.0.0.1:8081/live/<stream_key>.m3u8`

### 7.3 停播后没有回放

检查：

- `LIVE_RECORD_DIR` 是否和 SRS 录制目录一致
- backend 进程是否对该目录有读权限
- backend 日志是否收到 `on_dvr` 回调
- `live_sessions.record_file_path` 是否已写入有效录制文件路径
- backend 日志是否有录制文件不存在/读取失败错误

---

## 8. 快速自检命令

```bash
# 看 SRS 状态
docker ps | grep moevideo-srs

# 看 SRS 日志
docker logs --tail=200 moevideo-srs

# 看后端直播相关日志
journalctl -u moevideo-backend -f -o cat | rg "live|module=transcode"
```
