# syntax=docker/dockerfile:1.7

ARG DEBIAN_IMAGE=debian:13-slim
ARG GO_IMAGE=golang:1.26-bookworm

FROM ${DEBIAN_IMAGE} AS runtime-base

ENV DEBIAN_FRONTEND=noninteractive \
    BUN_INSTALL=/usr/local \
    PIPX_HOME=/opt/pipx \
    PIPX_BIN_DIR=/usr/local/bin \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    PATH=/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        ffmpeg \
        gosu \
        pipx \
        python3 \
        python3-pip \
        tini \
        unzip \
    && rm -rf /var/lib/apt/lists/*

# Install yt-dlp with curl-cffi via pipx.
RUN pipx install yt-dlp \
    && pipx inject yt-dlp curl-cffi \
    && yt-dlp --version \
    && pipx runpip yt-dlp show curl-cffi >/dev/null

# Install Bun runtime for Next.js and page resolver script.
RUN curl -fsSL https://bun.sh/install | bash \
    && ln -sf /usr/local/bin/bun /usr/local/bin/bunx \
    && bun --version

FROM runtime-base AS resolver-base
WORKDIR /app/scripts

COPY backend/scripts/package.json ./
RUN bun install

# Install Chromium and required runtime libs for Playwright resolver.
RUN bunx playwright install --with-deps chromium

COPY backend/scripts/ ./

FROM ${GO_IMAGE} AS backend-builder
WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/moevideo-backend ./cmd/server \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/moevideo-admin ./cmd/admin

FROM runtime-base AS frontend-builder
WORKDIR /src/frontend

ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080/api/v1
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}

COPY frontend/package.json frontend/bun.lock ./
RUN bun install

COPY frontend/ ./
RUN bun run build

FROM resolver-base AS backend-runtime
WORKDIR /app

COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY --from=backend-builder /out/moevideo-backend /app/moevideo-backend
COPY --from=backend-builder /out/moevideo-admin /app/moevideo-admin

ENV APP_ENV=production \
    HTTP_ADDR=:8080 \
    DB_PATH=/data/db/moevideo.db \
    LOCAL_STORAGE_DIR=/data/storage \
    TASK_TEMP_DIR=/data/temp \
    FFMPEG_BIN=ffmpeg \
    FFPROBE_BIN=ffprobe \
    YTDLP_BIN=yt-dlp \
    IMPORT_PAGE_RESOLVER_CMD=bun\ /app/scripts/page_manifest_resolver.mjs

EXPOSE 8080

ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
CMD ["/app/moevideo-backend"]

FROM runtime-base AS frontend-runtime
WORKDIR /app/frontend

COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY --from=frontend-builder /src/frontend/package.json ./package.json
COPY --from=frontend-builder /src/frontend/bun.lock ./bun.lock
COPY --from=frontend-builder /src/frontend/next.config.ts ./next.config.ts
COPY --from=frontend-builder /src/frontend/public ./public
COPY --from=frontend-builder /src/frontend/.next ./.next
COPY --from=frontend-builder /src/frontend/node_modules ./node_modules

ENV NODE_ENV=production \
    PORT=3000

EXPOSE 3000

ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
CMD ["bunx", "--bun", "next", "start", "-H", "0.0.0.0", "-p", "3000"]
