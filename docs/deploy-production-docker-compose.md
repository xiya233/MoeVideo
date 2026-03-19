# MoeVideo Docker Compose 生产部署指南

Docker Compose 部署方案，含宿主机 Nginx 反向代理、同域/分域配置模板。

支持两种模式：

1. 本地构建镜像（`docker compose up -d --build`）
2. 直接拉取 GHCR 预构建镜像（多架构 `amd64/arm64`）

## 1. 部署拓扑

默认启动 3 个容器服务：

1. `frontend`（Next.js，容器端口 `3000`）
2. `backend`（Go API + 导入/转码 Worker，容器端口 `8080`）
3. `redis`（限流与防刷）

## 2. 前置条件

在目标主机确认：

```bash
docker --version
docker compose version
```

如要走 HTTPS，请同时安装宿主机 Nginx + Certbot：

```bash
sudo apt update
sudo apt install -y nginx python3-certbot-nginx
sudo systemctl enable --now nginx
```

## 3. 克隆项目存储库并准备数据目录

```bash
git clone https://github.com/xiya233/MoeVideo.git
cd MoeVideo
mkdir -p data/db data/storage data/temp data/redis
```

数据目录说明：

- `./data/db`：SQLite 数据库
- `./data/storage`：媒体文件（封面/HLS/源文件）
- `./data/temp`：导入/上传探测/转码临时目录
- `./data/redis`：Redis 持久化数据

## 4. `.env.docker` 基础配置

根目录已提供全注释模板 `.env.docker`。上线前至少确认：

1. `JWT_SECRET`：替换为强随机密钥
2. `NEXT_PUBLIC_API_BASE_URL`：前端浏览器侧 API 地址（运行时注入）
3. `API_BASE_URL`：前端 SSR 请求 API 地址（建议与上一项一致）
4. `PUBLIC_BASE_URL`：后端对外基准地址
5. `CORS_ALLOWED_ORIGINS`：允许的前端来源
6. `AUTH_COOKIE_SECURE`：HTTPS 场景必须设为 `true`
7. `PUID/PGID`：容器进程写入 `./data` 的 UID/GID 映射

## 5. 同域/分域环境变量对照（.env.docker）

| 变量 | 同域（`example.com`） | 分域（`app.example.com` + `api.example.com`） | 说明 |
|---|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | `https://example.com/api/v1` | `https://api.example.com/api/v1` | 前端浏览器侧运行时优先读取 |
| `API_BASE_URL` | `https://example.com/api/v1` | `https://api.example.com/api/v1` | 前端 SSR 请求 API 地址 |
| `PUBLIC_BASE_URL` | `https://example.com` | `https://api.example.com` | 后端生成资源/API 对外 URL 基准 |
| `CORS_ALLOWED_ORIGINS` | `https://example.com` | `https://app.example.com` | 允许前端来源 |
| `AUTH_COOKIE_SECURE` | `true` | `true` | HTTPS 必须为 true |
| `AUTH_COOKIE_SAMESITE` | `lax` | `lax`（推荐） | app/api 同主域通常可用 lax |
| `AUTH_COOKIE_DOMAIN` | 留空 | 留空（推荐）或 `.example.com` | 留空更收敛；仅在需要跨子域共享时改 `.example.com` |

说明：

- 分域场景默认先用 `AUTH_COOKIE_DOMAIN=`（留空）+ `SameSite=lax`，更安全更易控。
- 只有明确需要跨子域共享 Cookie 时，再改 `AUTH_COOKIE_DOMAIN=.example.com`。

## 6. 首次构建并启动

```bash
docker compose --env-file .env.docker up -d --build
docker compose ps
```

## 6.1 使用 GHCR 预构建镜像启动

先设置镜像标签（默认 `latest`）：

```bash
echo "IMAGE_TAG=latest" >> .env.docker
```

然后使用覆盖文件启动：

```bash
docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml pull
docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml up -d --no-build
docker compose ps
```

日志查看：

```bash
docker compose logs -f backend
docker compose logs -f frontend
docker compose logs -f redis
```


## 8. 宿主机 Nginx 反向代理

### 8.1 同域模板（`example.com`）

创建 `/etc/nginx/sites-available/moevideo.conf`：

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

upstream moevideo_frontend {
    server 127.0.0.1:3000;
}

upstream moevideo_backend {
    server 127.0.0.1:8080;
}

server {
    listen 80;
    listen [::]:80;
    server_name example.com;
    client_max_body_size 2048m;

    location /api/ {
        proxy_pass http://moevideo_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }

    location /media/ {
        proxy_pass http://moevideo_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        proxy_pass http://moevideo_frontend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}
```

### 8.2 分域模板（`app.example.com` + `api.example.com`）

创建 `/etc/nginx/sites-available/moevideo.conf`：

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

upstream moevideo_frontend {
    server 127.0.0.1:3000;
}

upstream moevideo_backend {
    server 127.0.0.1:8080;
}

server {
    listen 80;
    listen [::]:80;
    server_name app.example.com;
    client_max_body_size 2048m;

    location / {
        proxy_pass http://moevideo_frontend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}

server {
    listen 80;
    listen [::]:80;
    server_name api.example.com;
    client_max_body_size 2048m;

    location /api/ {
        proxy_pass http://moevideo_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }

    location /media/ {
        proxy_pass http://moevideo_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /healthz {
        proxy_pass http://moevideo_backend/healthz;
    }
}
```

启用并加载：

```bash
sudo ln -s /etc/nginx/sites-available/moevideo.conf /etc/nginx/sites-enabled/moevideo.conf
sudo nginx -t
sudo systemctl reload nginx
```

## 9. HTTPS/证书（宿主机 Certbot）

同域：

```bash
sudo certbot --nginx -d example.com
```

分域：

```bash
sudo certbot --nginx -d app.example.com -d api.example.com
```

证书生效后，`.env.docker` 必须同步：

1. `AUTH_COOKIE_SECURE=true`
2. `PUBLIC_BASE_URL` 改为 `https://...`
3. `NEXT_PUBLIC_API_BASE_URL` 改为 `https://.../api/v1`
4. `API_BASE_URL` 改为 `https://.../api/v1`


## 10. 创建管理员账号

```bash
docker compose --env-file .env.docker run --rm backend \
  /app/moevideo-admin bootstrap \
  --email admin@example.com \
  --username admin \
  --password 'ChangeMe-StrongPassw0rd!' \
  --db /data/db/moevideo.db
```
该命令是幂等的：同邮箱重复执行会更新管理员信息。


## 11. yt-dlp推荐配置：

```
--concurrent-fragments 8 --fragment-retries 10 --retries 10 --socket-timeout 30 --force-ipv4 --impersonate Chrome-136
```


## 12. 日常运维与升级

重启：

```bash
docker compose restart backend frontend redis
```

升级：

```bash
git fetch --all --prune
git pull --ff-only origin main

# 本地构建:
docker compose --env-file .env.docker up -d --build

# GHCR:
docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml pull

docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml up -d --no-build

docker compose ps
docker compose logs --since=5m backend
```

## 13. 常见问题排查

### 13.1 前端 API 地址不对

```bash
grep NEXT_PUBLIC_API_BASE_URL .env.docker
grep API_BASE_URL .env.docker
docker compose --env-file .env.docker restart frontend
```


## 14. 安全建议

1. 必须替换 `JWT_SECRET`
2. HTTPS 场景必须 `AUTH_COOKIE_SECURE=true`
3. `CORS_ALLOWED_ORIGINS` 精确到实际的前端域名
4. 定期备份 `./data/db/moevideo.db` 与 `./data/storage`

