# MoeVideo

在线视频站点（VOD）基础实现。

- Frontend: Next.js App Router + Tailwind CSS（`frontend/`）
- Backend: Go + Fiber + SQLite WAL（`backend/`）
- Storage: local（默认）/ s3（环境变量切换）
- Tooling: bun + mise

## Quick Start

```bash
mise install
mise trust
```

### Backend

```bash
cp backend/.env.example backend/.env
mise run backend-dev
```

Backend 默认地址：`http://localhost:8080`

### Frontend

```bash
cp frontend/.env.example frontend/.env.local
mise run frontend-install
mise run frontend-dev
```

Frontend 默认地址：`http://localhost:3000`

## Verify

```bash
mise run backend-test
```

## API & Schema

- API 文档：`backend/docs/api.md`
- 数据模型文档：`backend/docs/schema.md`
- SQL 迁移：`backend/internal/db/migrations/0001_init.sql`
