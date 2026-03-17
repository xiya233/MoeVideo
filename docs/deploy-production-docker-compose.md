# MoeVideo Docker Compose 生产部署指南

本文档提供一套可直接落地的 Docker Compose 生产部署方案（本地构建，不发布 GHCR）。

## 1. 部署拓扑

本方案默认拉起 3 个服务：

1. `frontend`（Next.js，端口 `3000`）
2. `backend`（Go API + 导入/转码 Worker，端口 `8080`）
3. `redis`（限流与去重缓存）

说明：

- 本期 **不内置 Nginx**，可直接通过 `http://<host>:3000` 与 `http://<host>:8080` 访问。
- 如需 HTTPS，请在宿主机自行加反向代理（Nginx/Caddy/Traefik）。

---

## 2. 前置条件

在目标主机确认以下命令可用：

```bash
docker --version
docker compose version
```

如果命令不存在，请先安装 Docker Engine 与 Compose Plugin。

---

## 3. 代码与目录准备

```bash
# 1) 拉代码
git clone <your-repo-url> MoeVideo
cd MoeVideo

# 2) 创建数据目录 (所有持久数据统一放在 ./data)
mkdir -p data/db data/storage data/temp data/redis
```

数据目录用途：

- `./data/db`：SQLite 数据库
- `./data/storage`：本地媒体文件（封面/HLS/源文件等）
- `./data/temp`：导入/上传探测/转码临时目录
- `./data/redis`：Redis 数据

---

## 4. 配置 `.env.docker`

项目根目录已提供完整模板：`.env.docker`（含逐项注释）。

优先检查这些关键项：

1. `JWT_SECRET`：必须替换为强随机密钥
2. `NEXT_PUBLIC_API_BASE_URL`：前端构建时 API 地址
3. `PUBLIC_BASE_URL`：后端对外地址
4. `CORS_ALLOWED_ORIGINS`：允许前端来源
5. `AUTH_COOKIE_SECURE`：HTTPS 场景必须 `true`
6. `PUID/PGID`：容器内进程落盘权限映射

注意：

- `NEXT_PUBLIC_*` 是前端构建时变量，修改后必须重新 `--build`。
- 后端导入 fallback 命令默认已设为：
  - `IMPORT_PAGE_RESOLVER_CMD=bun /app/scripts/page_manifest_resolver.mjs`

---

## 5. 首次构建并启动

```bash
docker compose --env-file .env.docker up -d --build
```

查看状态：

```bash
docker compose ps
```

查看实时日志：

```bash
docker compose logs -f backend
docker compose logs -f frontend
docker compose logs -f redis
```

---

## 6. 运行后依赖自检

确认 backend 容器内依赖齐全（ffmpeg/yt-dlp/curl-cffi/Playwright）：

```bash
docker compose exec backend ffmpeg -version
docker compose exec backend ffprobe -version
docker compose exec backend yt-dlp --version
docker compose exec backend python3 -c "import curl_cffi; print('curl-cffi ok')"
docker compose exec backend bun --version
docker compose exec backend bash -lc "cd /app/scripts && bunx playwright --version"
```

健康检查：

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -I http://127.0.0.1:3000/
```

---

## 7. 优雅创建管理员账号（手动一次性）

使用后台 CLI 在容器内执行 bootstrap：

```bash
docker compose --env-file .env.docker run --rm backend \
  /app/moevideo-admin bootstrap \
  --email admin@example.com \
  --username admin \
  --password 'ChangeMe-StrongPassw0rd!' \
  --db /data/db/moevideo.db
```

说明：

- 该命令是幂等的：同邮箱重复执行会更新为管理员并覆盖密码。
- 执行后可使用前端 `/admin/login` 登录后台。

---

## 8. 日常运维

### 8.1 重启服务

```bash
docker compose restart backend
docker compose restart frontend
docker compose restart redis
```

### 8.2 停止/启动全栈

```bash
docker compose stop
docker compose start
```

### 8.3 查看容器资源占用

```bash
docker stats
```

---

## 9. 升级发布流程

```bash
# 1) 拉取最新代码
git fetch --all --prune
git pull --ff-only origin main

# 2) 复查配置变化（重点看 backend/.env.example 新增项）
git diff HEAD~1 -- backend/.env.example frontend/.env.example

# 3) 重新构建并滚动重启
docker compose --env-file .env.docker up -d --build

# 4) 验证
docker compose ps
docker compose logs --since=5m backend
```

---

## 10. 常见问题排查

### 10.1 前端 API 地址不对

现象：前端请求到了错误后端地址。  
排查：

```bash
grep NEXT_PUBLIC_API_BASE_URL .env.docker
docker compose --env-file .env.docker build frontend --no-cache
docker compose --env-file .env.docker up -d frontend
```

### 10.2 resolver 报 `bun: executable file not found`

说明 `IMPORT_PAGE_RESOLVER_CMD` 与容器路径不匹配。  
本方案应使用：

```env
IMPORT_PAGE_RESOLVER_CMD=bun /app/scripts/page_manifest_resolver.mjs
```

### 10.3 `no space left on device`

优先检查 `./data/temp` 所在磁盘空间：

```bash
df -h
du -sh data/temp data/storage data/db data/redis
```

### 10.4 权限报错（PUID/PGID）

确认宿主机用户 UID/GID：

```bash
id
```

把 `.env.docker` 中 `PUID`/`PGID` 改为对应值后重启：

```bash
docker compose --env-file .env.docker up -d
```

---

## 11. 安全建议（生产）

1. 生产务必替换 `JWT_SECRET` 强随机值。
2. 上 HTTPS 后将 `AUTH_COOKIE_SECURE=true`。
3. `CORS_ALLOWED_ORIGINS` 精确到你的前端域名，避免 `*`。
4. 定期备份 `./data/db/moevideo.db` 与 `./data/storage`。
5. 如不需要对外暴露 backend，可仅暴露 frontend 端口并加宿主机反代。

---

## 12. 本期限制

1. 本期不做 GHCR 镜像发布，统一本地 `docker compose build`。
2. 本期不内置 Nginx/SSL。
3. ARM 平台在 Playwright Chromium 依赖上可能需要额外兼容验证。
