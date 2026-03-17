# MoeVideo

在线视频站点（VOD）基础实现。

- Frontend: Next.js App Router + Tailwind CSS（`frontend/`）
- Backend: Go + Fiber + SQLite WAL（`backend/`）
- Storage: local（默认）/ s3（环境变量切换）
- Tooling: bun + mise
- Transcode: ffmpeg/ffprobe（后端内置 HLS 多码率 Worker）

## Quick Start

```bash
mise install
mise trust
```

### Backend

```bash
cp backend/.env.example backend/.env
# 必须先在 backend/.env 里替换 JWT_SECRET（不能使用默认占位值）
mise run backend-dev
```

Backend 默认地址：`http://localhost:8080`
后端程序会自动读取 `backend/.env`（同名系统环境变量优先，不会被 `.env` 覆盖）。

若要启用 URL 导入的页面解析 fallback（Playwright）：

```bash
cd backend/scripts
bun install
bunx playwright install chromium
```

说明：项目通过 npm alias 使用 `rebrowser-playwright@1.52.0`（包名保持 `playwright`）。

初始化管理员（一次性）：

```bash
cd backend
go run ./cmd/admin bootstrap --email admin@example.com --username admin --password your-password
```

### Frontend

```bash
cp frontend/.env.example frontend/.env.local
mise run frontend-install
mise run frontend-dev
```

Frontend 默认地址：`http://localhost:3000`
后台入口：`http://localhost:3000/admin/login`

## Verify

```bash
mise run backend-test
```

## API & Schema

- API 文档：`backend/docs/api.md`
- 数据模型文档：`backend/docs/schema.md`
- yt-dlp 参数配置文档：`backend/docs/ytdlp-settings.md`
- yt-dlp 用户 Cookies 使用文档：`backend/docs/ytdlp-user-cookies.md`
- Debian 13 生产部署文档：`docs/deploy-production-debian13.md`
- Docker Compose 生产部署文档（含宿主机 Nginx、同域/分域配置）：`docs/deploy-production-docker-compose.md`
- SQL 迁移：`backend/internal/db/migrations/0001_init.sql`

生产部署建议：可使用 `mise` 安装并锁定工具链版本（Go/Bun），服务运行请使用 `systemd` 原生命令（不要用 `mise run`）。

## Behavior Notes

- 上传发布、URL 导入、BT 导入现在都要求必须选择分类（`category_id` 必填）。
