# MoeVideo Docker Compose 生产部署指南

本文档提供一套可直接落地的 Docker Compose 生产部署方案，支持两种模式：

1. 本地构建镜像（`docker compose up -d --build`）
2. 直接拉取 GHCR 预构建镜像（多架构 `amd64/arm64`）

并包含宿主机 Nginx 反向代理、同域/分域配置模板。

## 1. 部署拓扑

本方案默认启动 3 个容器服务：

1. `frontend`（Next.js，容器端口 `3000`）
2. `backend`（Go API + 导入/转码 Worker，容器端口 `8080`）
3. `redis`（限流与防刷缓存）

本期不在 compose 内置 Nginx；Nginx 部署在宿主机，负责 HTTPS 与反向代理。

## 2. 前置条件

在目标主机确认：

```bash
docker --version
docker compose version
```

如要走 HTTPS，请同时安装宿主机 Nginx + Certbot：

```bash
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx
sudo systemctl enable --now nginx
```

## 3. 代码与数据目录准备

```bash
git clone <your-repo-url> MoeVideo
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

## 6.1 使用 GHCR 预构建镜像启动（推荐线上）

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

若要切换版本，例如 `v1.2.3`：

```bash
sed -i 's/^IMAGE_TAG=.*/IMAGE_TAG=v1.2.3/' .env.docker
docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml pull
docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml up -d --no-build
```

GHCR 标签约定（由 GitHub Actions 自动发布）：

1. `main` 分支推送：`latest` + `sha-<shortsha>`
2. `v*` 标签推送：`vX.Y.Z`（并附 `vX.Y`）

日志查看：

```bash
docker compose logs -f backend
docker compose logs -f frontend
docker compose logs -f redis
```

## 7. 依赖自检（容器内）

确认 backend 镜像内关键依赖完整：

```bash
docker compose exec backend ffmpeg -version
docker compose exec backend ffprobe -version
docker compose exec backend yt-dlp --version
docker compose exec backend bash -lc "pipx runpip yt-dlp show curl-cffi >/dev/null && echo curl-cffi-ok"
docker compose exec backend bun --version
docker compose exec backend bash -lc "cd /app/scripts && bunx playwright --version"
```

说明：`curl-cffi` 是注入到 `pipx` 的 `yt-dlp` 虚拟环境，不在系统 `python3` 全局包路径中。
说明：镜像构建已改为 `bunx playwright install chromium`（不使用 `--with-deps`），Chromium 运行依赖由 Debian apt 预装，避免 Debian 13 下 Playwright fallback 依赖脚本拉取已废弃包而失败。

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
sudo ln -sf /etc/nginx/sites-available/moevideo.conf /etc/nginx/sites-enabled/moevideo.conf
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

## 10. 前端 API 运行时变量说明（重要）

前端容器启动时会自动生成 `/runtime-env.js`，浏览器侧优先读取运行时
`NEXT_PUBLIC_API_BASE_URL`。  
因此修改 API 地址后，不需要重建 frontend 镜像，只需重启 frontend 容器。

推荐做法（同域 <-> 分域切换）：

```bash
# 1) 修改 .env.docker 中 NEXT_PUBLIC_API_BASE_URL / API_BASE_URL / CORS 等变量
vim .env.docker

# 2) 仅重启 frontend（backend 是否重启按你的变更决定）
docker compose --env-file .env.docker restart frontend

# 3) 验证 frontend 运行时注入结果
docker compose --env-file .env.docker exec frontend cat /app/frontend/public/runtime-env.js
```

说明：Dockerfile 的 `NEXT_PUBLIC_API_BASE_URL` build-arg 仍保留，作为运行时变量缺失时的兜底值。

## 11. 优雅创建管理员账号（手动一次性）

```bash
docker compose --env-file .env.docker run --rm backend \
  /app/moevideo-admin bootstrap \
  --email admin@example.com \
  --username admin \
  --password 'ChangeMe-StrongPassw0rd!' \
  --db /data/db/moevideo.db
```

该命令是幂等的：同邮箱重复执行会更新管理员信息。

## 12. 上线后验证清单（按模式）

### 12.1 同域验证

```bash
curl -I https://example.com
curl -I https://example.com/api/v1/home
```

### 12.2 分域验证

```bash
curl -I https://app.example.com
curl -I https://api.example.com/api/v1/home
```

### 12.3 浏览器验证（两种模式都做）

1. 登录后请求能带上 Cookie（开发者工具 Network 可见）
2. 控制台无 CORS 报错
3. 视频页封面与 `/media/...` 资源可正常加载

## 13. 日常运维与升级

重启：

```bash
docker compose restart backend frontend redis
```

升级：

```bash
git fetch --all --prune
git pull --ff-only origin main
# 本地构建模式:
docker compose --env-file .env.docker up -d --build
# GHCR 模式:
# docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml pull
# docker compose --env-file .env.docker -f docker-compose.yml -f docker-compose.ghcr.yml up -d --no-build
docker compose ps
docker compose logs --since=5m backend
```

## 14. 常见问题排查

### 14.1 前端 API 地址不对

```bash
grep NEXT_PUBLIC_API_BASE_URL .env.docker
grep API_BASE_URL .env.docker
docker compose --env-file .env.docker restart frontend
```

### 14.2 resolver 报 `bun: executable file not found`

确保：

```env
IMPORT_PAGE_RESOLVER_CMD=bun /app/scripts/page_manifest_resolver.mjs
```

### 14.3 `no space left on device`

```bash
df -h
du -sh data/temp data/storage data/db data/redis
```

### 14.4 PUID/PGID 权限问题

```bash
id
docker compose --env-file .env.docker up -d
```

### 14.5 frontend 构建报 `... /src/frontend/public: not found`

原因通常是 `frontend/public` 为空目录且未被上下文携带。当前 Dockerfile 已在构建阶段自动创建该目录，并在仓库中保留了 `frontend/public/.gitkeep` 作为占位。  
若你新增静态资源，请放到 `frontend/public` 并提交到仓库。

## 15. 安全建议

1. 生产必须替换 `JWT_SECRET`
2. HTTPS 场景必须 `AUTH_COOKIE_SECURE=true`
3. `CORS_ALLOWED_ORIGINS` 精确到实际前端域名
4. 定期备份 `./data/db/moevideo.db` 与 `./data/storage`

## 16. 本期限制

1. GHCR 镜像仅做基础发布（未启用签名/provenance/SBOM）
2. 本期不在 compose 内置 Nginx 服务
3. ARM 平台 Playwright Chromium 需自行兼容验证
