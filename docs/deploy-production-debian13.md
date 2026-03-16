# MoeVideo Debian 13 生产部署手册

本文档面向 Debian 13（Trixie）服务器，目标是从零部署 MoeVideo 到生产环境。  
默认推荐同域部署：`https://example.com`（前后端同域，Nginx 统一反代）。

## 1. 部署架构与端口规划

- `Nginx`：`443/80`（TLS 与反向代理入口）
- `Frontend (Next.js)`：`127.0.0.1:3000`
- `Backend (Go/Fiber)`：`127.0.0.1:8080`
- `Redis`：`127.0.0.1:6379`
- `SQLite`：本地文件（默认 `./data/db/moevideo.db`，相对 backend 工作目录）

流量路径：

1. 浏览器 -> Nginx (`443`)
2. `/api/` 与 `/media/` -> Backend (`8080`)
3. 其他路径 -> Frontend (`3000`)

## 2. Debian 13 初始化

以下命令默认用 `root` 执行（或 `sudo`）。

### 2.1 创建运行用户与目录

```bash
adduser --disabled-password --gecos "" moevideo
mkdir -p /opt/moevideo
chown -R moevideo:moevideo /opt/moevideo
mkdir -p /opt/moevideo/backend/data/db /opt/moevideo/backend/data/storage
chown -R moevideo:moevideo /opt/moevideo/backend/data
chmod 750 /opt/moevideo/backend/data /opt/moevideo/backend/data/db /opt/moevideo/backend/data/storage
```

### 2.2 安装系统依赖

```bash
apt update
apt install -y \
  curl ca-certificates git build-essential unzip jq \
  python3 python3-pip pipx \
  ffmpeg redis-server nginx certbot python3-certbot-nginx
```

初始化 `pipx` 路径：

```bash
pipx ensurepath
```

如果你启用了防火墙（`ufw` 或云安全组），请额外放行 BT 监听端口（示例 `51413`）：

```bash
ufw allow 51413/tcp
ufw allow 51413/udp
```

## 3. 安装工具链（`mise` 主路径）

本手册默认用 `mise` 安装 Go/Bun。  
`mise` 只负责安装工具链，运行服务仍走 systemd 原生命令（不使用 `mise run`）。

## 3.1 安装 `mise`

```bash
su - moevideo
curl -fsSL https://mise.run | sh
echo 'eval "$(~/.local/bin/mise activate bash)"' >> ~/.bashrc
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

验证：

```bash
mise --version
```

## 3.2 锁定 `mise.toml` 版本（生产禁止 `latest`）

> 生产环境不要使用 `latest`，必须固定版本，避免后续自动漂移。

在仓库根目录编辑 `mise.toml`，将工具版本锁定为明确版本号，例如：

```toml
[tools]
bun = "1.3.20"
go = "1.26.0"
```

## 3.3 用 `mise` 安装 Go/Bun

```bash
cd /opt/moevideo
mise install
mise ls
```

为了让 systemd 使用“原生固定路径命令”，创建稳定软链接：

```bash
mkdir -p /opt/moevideo/bin
ln -sfn ~/.local/share/mise/installs/bun/1.3.20/bin/bun /opt/moevideo/bin/bun
ln -sfn ~/.local/share/mise/installs/go/1.26.0/bin/go /opt/moevideo/bin/go
```

校验：

```bash
/opt/moevideo/bin/go version
/opt/moevideo/bin/bun --version
```

## 3.4 用 pipx 安装 yt-dlp + curl-cffi（必做）

```bash
su - moevideo
python3 -m pipx ensurepath
export PATH="$HOME/.local/bin:$PATH"
pipx install yt-dlp
pipx inject yt-dlp curl-cffi
yt-dlp --version
python3 -c "import curl_cffi; print(curl_cffi.__version__)"
```

> 后续 `YTDLP_BIN` 建议写绝对路径：`/home/moevideo/.local/bin/yt-dlp`

## 3.5 安装 Playwright 浏览器（resolver fallback 需要）

```bash
cd /opt/moevideo
git clone <你的仓库地址> .
cd backend/scripts
bun install
bunx playwright install chromium
```

> 必须在运行服务的同一用户（`moevideo`）下执行，确保浏览器缓存权限正确。

## 3.6 手动安装 Go/Bun 回退路径（可选）

如果你的环境无法使用 `mise`，可用手动安装方式。  
该回退仅用于“安装工具”，服务运行方式仍按 systemd 原生命令。

### Bun 手动安装

```bash
su - moevideo
curl -fsSL https://bun.sh/install | bash
echo 'export PATH="$HOME/.bun/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

### Go 手动安装

```bash
exit
GO_VERSION=1.26.0
ARCH=linux-amd64
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.${ARCH}.tar.gz" -o /tmp/go.tgz
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH=/usr/local/go/bin:$PATH' >/etc/profile.d/go.sh
chmod +x /etc/profile.d/go.sh
```

为保持后续命令一致（仍使用 `/opt/moevideo/bin/*`）：

```bash
su - moevideo
mkdir -p /opt/moevideo/bin
ln -sfn /usr/local/go/bin/go /opt/moevideo/bin/go
ln -sfn /home/moevideo/.bun/bin/bun /opt/moevideo/bin/bun
```

## 4. 拉代码与构建

下面继续使用 `moevideo` 用户：

```bash
su - moevideo
cd /opt/moevideo
export PATH="/opt/moevideo/bin:$HOME/.local/bin:$PATH"
```

重要说明（Next.js 生产模式）：

- 前端 `NEXT_PUBLIC_*` 变量在 `build` 时注入产物，不是 `start` 时动态读取。
- 首次部署或修改前端环境变量后，必须先完成 **5.2 前端环境配置**，再执行 4.2 前端构建并重启前端服务。

## 4.1 Backend 构建

```bash
cd /opt/moevideo/backend
/opt/moevideo/bin/go mod download
mkdir -p /opt/moevideo/bin
/opt/moevideo/bin/go build -trimpath -ldflags="-s -w" -o /opt/moevideo/bin/moevideo-backend ./cmd/server
/opt/moevideo/bin/go build -trimpath -ldflags="-s -w" -o /opt/moevideo/bin/moevideo-admin ./cmd/admin
```

## 4.2 Frontend 构建

```bash
cd /opt/moevideo/frontend
/opt/moevideo/bin/bun install
/opt/moevideo/bin/bun run build
```

## 5. 生产环境变量配置（详细注释版）

## 5.1 Backend：`/opt/moevideo/backend/.env`

创建文件：

```bash
cat >/opt/moevideo/backend/.env <<'EOF'
# 运行环境：production 会强制一些安全策略（例如 Cookie Secure）
APP_ENV=production

# 日志级别：debug / info / warn / error（生产推荐 info）
LOG_LEVEL=info

# 后端监听地址（仅本机监听，交给 Nginx 反代）
HTTP_ADDR=127.0.0.1:8080

# SQLite 数据库文件路径
DB_PATH=./data/db/moevideo.db

# JWT 签名密钥（必须是强随机字符串，不能是占位值）
JWT_SECRET=REPLACE_WITH_LONG_RANDOM_SECRET

# Access Token 有效期（短）
ACCESS_TOKEN_TTL=15m

# Refresh Token 有效期（长）
REFRESH_TOKEN_TTL=720h

# Cookie 作用域：
# 同域部署可留空；分域部署可设为 .example.com
AUTH_COOKIE_DOMAIN=

# 生产环境必须为 true（APP_ENV=production 时也会强制 true）
AUTH_COOKIE_SECURE=true

# SameSite：同域推荐 lax；跨站前后端分域通常需 none（且 Secure=true）
AUTH_COOKIE_SAMESITE=lax

# Cookie 路径，通常保持 /
AUTH_COOKIE_PATH=/

# 允许跨域来源（逗号分隔）
# 同域部署通常填主站域名
CORS_ALLOWED_ORIGINS=https://example.com

# 限流开关
RATE_LIMIT_ENABLED=true

# Redis 地址（限流与防刷依赖）
RATE_LIMIT_REDIS_ADDR=127.0.0.1:6379

# Redis 密码（如未配置 Redis 密码可留空）
RATE_LIMIT_REDIS_PASSWORD=

# Redis DB 编号
RATE_LIMIT_REDIS_DB=0

# 限流键前缀（多环境可区分）
RATE_LIMIT_REDIS_PREFIX=moevideo-prod

# 生产 Redis 故障时是否拒绝写操作（推荐 true）
RATE_LIMIT_FAIL_CLOSED_PROD=true

# 开发环境 Redis 异常时是否回退内存限流（生产一般无效）
RATE_LIMIT_DEV_FALLBACK_MEMORY=false

# 存储驱动：local 或 s3
STORAGE_DRIVER=local

# 本地存储目录（local 模式）
LOCAL_STORAGE_DIR=./data/storage

# 安全建议：数据库目录与媒体目录必须分开；
# 不要把 DB_PATH 放到 LOCAL_STORAGE_DIR 目录下。

# 对外可访问的后端基地址（用于拼接媒体访问 URL）
PUBLIC_BASE_URL=https://example.com

# 单文件上传大小上限（MB）
MAX_UPLOAD_MB=2048

# 上传预签名链接有效期
UPLOAD_URL_EXPIRES=15m

# ffmpeg 与 ffprobe 可执行路径
FFMPEG_BIN=/usr/bin/ffmpeg
FFPROBE_BIN=/usr/bin/ffprobe

# 转码 worker 轮询间隔与重试次数
TRANSCODE_POLL_INTERVAL=1s
TRANSCODE_MAX_RETRIES=3
# 转码处理中的进度心跳日志间隔（建议 5s）
TRANSCODE_PROGRESS_LOG_INTERVAL=5s

# 导入 worker 轮询间隔与重试次数
IMPORT_POLL_INTERVAL=1s
IMPORT_MAX_RETRIES=3
# 导入下载过程进度日志间隔（建议 5s）
IMPORT_PROGRESS_LOG_INTERVAL=5s

# BT 种子大小上限（MB）
IMPORT_TORRENT_MAX_MB=2

# 单次 BT 导入允许勾选的最大文件数
IMPORT_MAX_SELECTED_FILES=20

# BT 是否允许上传（生产提速建议 true）
IMPORT_BT_ENABLE_UPLOAD=true

# BT 固定监听端口（需在防火墙/安全组放行 TCP/UDP）
IMPORT_BT_LISTEN_PORT=51413

# BT 是否启用端口映射能力（生产建议 true）
IMPORT_BT_ENABLE_PORT_FORWARD=true

# BT 文件读取预读窗口（MB），过小更容易出现吞吐抖动
IMPORT_BT_READER_READAHEAD_MB=32

# BT 速度平滑窗口（秒），用于前端展示与进度日志
IMPORT_BT_SPEED_SMOOTH_WINDOW_SEC=5

# yt-dlp 可执行路径（建议写绝对路径）
YTDLP_BIN=/home/moevideo/.local/bin/yt-dlp

# URL 导入超时时间（秒）
IMPORT_URL_TIMEOUT_SEC=600

# URL 导入最大时长（秒），0=不限制
IMPORT_URL_MAX_DURATION_SEC=0

# URL 导入最大文件大小（MB），0=不限制
IMPORT_URL_MAX_FILE_MB=0

# 页面解析 fallback（Playwright）开关
IMPORT_PAGE_RESOLVER_ENABLED=true

# 页面解析超时时间（秒）
IMPORT_PAGE_RESOLVER_TIMEOUT_SEC=25

# 页面解析最多保留候选媒体链接数
IMPORT_PAGE_RESOLVER_MAX_CANDIDATES=20

# 页面解析命令（在 backend 目录执行）
IMPORT_PAGE_RESOLVER_CMD=bun scripts/page_manifest_resolver.mjs

# S3 模式可选配置（STORAGE_DRIVER=s3 时启用）
# S3_BUCKET=moevideo
# S3_REGION=ap-southeast-1
# S3_ENDPOINT=
# S3_ACCESS_KEY_ID=
# S3_SECRET_ACCESS_KEY=
# S3_SESSION_TOKEN=
# S3_FORCE_PATH_STYLE=false
# S3_PUBLIC_BASE_URL=
EOF
```

## 5.2 Frontend：`/opt/moevideo/frontend/.env.production.local`

同域部署：

```bash
cat >/opt/moevideo/frontend/.env.production.local <<'EOF'
# 浏览器访问 API 的基地址（同域走 https://example.com/api/v1）
NEXT_PUBLIC_API_BASE_URL=https://example.com/api/v1
EOF
```

分域部署（例如 `app.example.com` + `api.example.com`）：

```env
NEXT_PUBLIC_API_BASE_URL=https://api.example.com/api/v1
```

## 6. systemd 进程守护

生产约束：systemd 的 `ExecStart` 直接写原生命令（固定二进制路径），不要写 `mise run ...`。

## 6.1 Backend unit

创建 `/etc/systemd/system/moevideo-backend.service`：

```ini
[Unit]
Description=MoeVideo Backend API
After=network.target redis-server.service
Wants=redis-server.service

[Service]
Type=simple
User=moevideo
Group=moevideo
WorkingDirectory=/opt/moevideo/backend
EnvironmentFile=/opt/moevideo/backend/.env
Environment=PATH=/opt/moevideo/bin:/home/moevideo/.local/bin:/usr/bin:/bin
SyslogIdentifier=moevideo-backend
StandardOutput=journal
StandardError=journal
ExecStart=/opt/moevideo/bin/moevideo-backend
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

## 6.2 Frontend unit

创建 `/etc/systemd/system/moevideo-frontend.service`：

```ini
[Unit]
Description=MoeVideo Frontend (Next.js)
After=network.target

[Service]
Type=simple
User=moevideo
Group=moevideo
WorkingDirectory=/opt/moevideo/frontend
EnvironmentFile=/opt/moevideo/frontend/.env.production.local
Environment=PORT=3000
Environment=NODE_ENV=production
Environment=PATH=/opt/moevideo/bin:/usr/bin:/bin
ExecStart=/opt/moevideo/bin/bun run start
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

## 6.3 启动服务

```bash
systemctl daemon-reload
systemctl enable redis-server moevideo-backend moevideo-frontend
systemctl start redis-server moevideo-backend moevideo-frontend

systemctl status moevideo-backend --no-pager
systemctl status moevideo-frontend --no-pager
```

日志查看：

```bash
journalctl -u moevideo-backend -f -o cat
journalctl -u moevideo-frontend -f
```

## 7. Nginx 反向代理配置（含 API / Media / WebSocket）

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

        # WebSocket (弹幕等)
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
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

启用站点：

```bash
ln -sf /etc/nginx/sites-available/moevideo.conf /etc/nginx/sites-enabled/moevideo.conf
nginx -t
systemctl reload nginx
```

## 8. SSL 证书（Certbot）

```bash
certbot --nginx -d example.com
```

验证自动续期：

```bash
systemctl status certbot.timer --no-pager
certbot renew --dry-run
```

## 9. 首次上线验收清单

## 9.1 基础健康检查

```bash
curl -i http://127.0.0.1:8080/healthz
curl -I https://example.com
curl -I https://example.com/api/v1/home
```

## 9.2 功能检查

1. 打开站点首页、播放页、后台登录页。  
2. 完成一次注册/登录。  
3. 上传一个视频并确认转码成功。  
4. 跑一次 URL 导入与 BT 导入。  
5. 验证弹幕 WebSocket 在 Nginx 下正常。  

## 9.3 管理员初始化（如未创建）

```bash
su - moevideo
cd /opt/moevideo/backend
/opt/moevideo/bin/moevideo-admin bootstrap \
  --email admin@example.com \
  --username admin \
  --password 'ReplaceWithStrongPassword'
```

## 9.4 工具链版本锁定校验

```bash
su - moevideo
cd /opt/moevideo
mise ls
/opt/moevideo/bin/go version
/opt/moevideo/bin/bun --version
```

验收标准：

- `mise ls` 显示固定版本（非 `latest`）。
- `go` 和 `bun` 与你在 `mise.toml` 锁定的版本一致。

## 10. 运维常用命令与故障排查

## 10.1 常用命令

```bash
# 查看服务状态
systemctl status moevideo-backend moevideo-frontend redis-server nginx --no-pager

# 重启服务
systemctl restart moevideo-backend
systemctl restart moevideo-frontend
systemctl reload nginx

# 追日志
journalctl -u moevideo-backend -f -o cat
journalctl -u moevideo-backend --since "10 min ago" | rg "module=import|module=transcode"
journalctl -u moevideo-frontend -f
tail -f /var/log/nginx/error.log
```

## 10.2 常见问题

1. `JWT_SECRET must be set...`  
   - 检查 `/opt/moevideo/backend/.env` 是否存在且 `JWT_SECRET` 不是占位值。

2. `yt-dlp not found`  
   - 确认 `YTDLP_BIN=/home/moevideo/.local/bin/yt-dlp`，并且该文件可执行。

3. URL fallback 失败（Playwright）  
   - 确认在 `moevideo` 用户下执行过 `cd backend/scripts && bun install && bunx playwright install chromium`。

4. 登录态异常（Cookie 不生效）  
   - 检查 `AUTH_COOKIE_SECURE`、`AUTH_COOKIE_SAMESITE`、`CORS_ALLOWED_ORIGINS`、Nginx `X-Forwarded-Proto`。

5. 429 频繁  
   - 检查 Redis 是否可用；查看 backend 日志中的 `rate_limit` 规则 ID，再按需调节限流参数。

## 10.3 工具链升级流程（Go/Bun）

建议流程：

1. 修改 `mise.toml` 中 Go/Bun 版本号（固定版本，不写 `latest`）。
2. 在预发布环境执行 `mise install`，完成构建与回归测试。
3. 生产执行：
   - `mise install`
   - 更新 `/opt/moevideo/bin/go`、`/opt/moevideo/bin/bun` 软链接
   - 重新构建 backend/frontend
   - `systemctl restart moevideo-backend moevideo-frontend`
4. 观察日志与核心链路（登录、上传、播放、导入）后再全量推广。

## 10.4 生产更新流程（代码发布/回滚）

本节用于日常“拉新代码并上线”的标准流程，适用于 systemd 运行方式。

### 10.4.1 标准更新步骤（逐条执行）

先切到运行用户：

```bash
su - moevideo
```

定义目录变量（按你的实际路径修改）：

```bash
APP_DIR=/opt/moevideo/MoeVideo
BACKEND_DIR=$APP_DIR/backend
FRONTEND_DIR=$APP_DIR/frontend
```

1) 检查当前状态：

```bash
cd "$APP_DIR"
git status -sb
git rev-parse --short HEAD
systemctl status moevideo-backend moevideo-frontend --no-pager
```

2) 备份（上线前必须）：

```bash
cd "$APP_DIR"
TS=$(date +%F-%H%M%S)
cp "$BACKEND_DIR/data/db/moevideo.db" "$BACKEND_DIR/data/db/moevideo.db.bak-$TS"
cp "$BACKEND_DIR/.env" "$BACKEND_DIR/.env.bak-$TS"
cp "$FRONTEND_DIR/.env.production.local" "$FRONTEND_DIR/.env.production.local.bak-$TS"
```

3) 拉取代码：

```bash
cd "$APP_DIR"
git fetch --all --prune
git log --oneline --decorate HEAD..origin/main
git pull --ff-only origin main
git rev-parse --short HEAD
```

4) 对齐新增环境变量：

```bash
cd "$APP_DIR"
comm -23 \
  <(grep -E '^[A-Z0-9_]+=' "$BACKEND_DIR/.env.example" | cut -d= -f1 | sort -u) \
  <(grep -E '^[A-Z0-9_]+=' "$BACKEND_DIR/.env" | cut -d= -f1 | sort -u)
```

如果有输出，表示这些 key 在 `backend/.env` 里缺失，需要先补齐再继续发布。

如果本次更新包含前端 `NEXT_PUBLIC_*` 变量变更，先更新 `frontend/.env.production.local`，再执行前端 build（`NEXT_PUBLIC_*` 是 build-time 注入）。

5) 重建 backend：

```bash
cd "$BACKEND_DIR"
/opt/moevideo/bin/go mod download
/opt/moevideo/bin/go build -trimpath -ldflags="-s -w" -o /opt/moevideo/bin/moevideo-backend ./cmd/server
```

6) 重建 frontend：

```bash
cd "$FRONTEND_DIR"
/opt/moevideo/bin/bun install
/opt/moevideo/bin/bun run build
```

7) 条件步骤（仅当 URL resolver 相关依赖有变动）：

```bash
cd "$APP_DIR"
git diff --name-only ORIG_HEAD..HEAD | rg '^backend/scripts/' || true
```

若有输出，再执行：

```bash
cd "$BACKEND_DIR/scripts"
/opt/moevideo/bin/bun install
PATH=/opt/moevideo/bin:$PATH /opt/moevideo/bin/bunx playwright install chromium
```

8) 重启服务：

```bash
systemctl restart moevideo-backend
systemctl restart moevideo-frontend
```

如 Nginx 配置有改动，再执行：

```bash
nginx -t && systemctl reload nginx
```

9) 发布后验收：

```bash
curl -i http://127.0.0.1:8080/healthz
curl -I https://your-domain
systemctl status moevideo-backend moevideo-frontend --no-pager
journalctl -u moevideo-backend -f -o cat
journalctl -u moevideo-frontend -f -o cat
```

### 10.4.2 快速回滚步骤（失败立即恢复）

1) 找到回滚目标 commit（稳定版本）：

```bash
cd "$APP_DIR"
git reflog --date=iso -n 20
```

2) 回滚代码：

```bash
cd "$APP_DIR"
git reset --hard <stable_commit>
```

3) 重新构建并重启：

```bash
cd "$BACKEND_DIR"
/opt/moevideo/bin/go build -trimpath -ldflags="-s -w" -o /opt/moevideo/bin/moevideo-backend ./cmd/server

cd "$FRONTEND_DIR"
/opt/moevideo/bin/bun install
/opt/moevideo/bin/bun run build

systemctl restart moevideo-backend
systemctl restart moevideo-frontend
```

4) 若确认是数据问题，使用备份恢复数据库：

```bash
systemctl stop moevideo-backend
cp "$BACKEND_DIR/data/db/moevideo.db.bak-<timestamp>" "$BACKEND_DIR/data/db/moevideo.db"
systemctl start moevideo-backend
```

5) 复用“发布后验收”同一组检查确认恢复成功。

### 10.4.3 BT 提速版本更新注意事项

发布后确认以下配置已在 `backend/.env` 生效：

- `IMPORT_BT_ENABLE_UPLOAD`
- `IMPORT_BT_LISTEN_PORT`
- `IMPORT_BT_ENABLE_PORT_FORWARD`
- `IMPORT_BT_READER_READAHEAD_MB`
- `IMPORT_BT_SPEED_SMOOTH_WINDOW_SEC`

若启用了防火墙/安全组，放行 `IMPORT_BT_LISTEN_PORT`（TCP/UDP）：

```bash
ufw allow 51413/tcp
ufw allow 51413/udp
```

验证日志里出现 BT 新配置启动信息：

```bash
journalctl -u moevideo-backend --since "10 min ago" -o cat | rg "torrent client initialized|torrent transfer progress"
```

---

## 附录 A：分域部署差异配置

示例域名：

- 前端：`https://app.example.com`
- 后端：`https://api.example.com`

Backend `.env` 关键差异：

```env
PUBLIC_BASE_URL=https://api.example.com
AUTH_COOKIE_DOMAIN=.example.com
AUTH_COOKIE_SECURE=true
AUTH_COOKIE_SAMESITE=none
CORS_ALLOWED_ORIGINS=https://app.example.com
```

Frontend `.env.production.local`：

```env
NEXT_PUBLIC_API_BASE_URL=https://api.example.com/api/v1
```

Nginx 可按域名拆成两个 server 块（一个前端域、一个 API 域），核心要求不变：  
Cookie 域一致、CORS 白名单精确、HTTPS 全链路开启。
