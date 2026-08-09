# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src
COPY frontend/admin/package.json frontend/admin/pnpm-lock.yaml frontend/admin/pnpm-workspace.yaml ./frontend/admin/
RUN corepack enable && cd frontend/admin && pnpm install --frozen-lockfile --ignore-scripts
COPY frontend/admin ./frontend/admin
RUN cd frontend/admin && pnpm build

FROM node:22-alpine AS renderer
WORKDIR /src/frontend/renderer
COPY frontend/renderer/package.json frontend/renderer/package-lock.json ./
RUN npm ci --omit=dev
COPY frontend/renderer/src ./src

FROM node:22-alpine AS theme
WORKDIR /src/themes/default
COPY themes/default/package.json themes/default/package-lock.json ./
RUN npm ci
COPY themes/default/ ./
RUN NODE_OPTIONS=--max-old-space-size=2048 npm run build && rm -rf node_modules

FROM golang:1.23-alpine AS build
WORKDIR /src
ARG VERSION=dev
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend ./backend
COPY --from=web /src/backend/web/admin ./backend/web/admin
RUN cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/blog-server ./cmd/blog

FROM node:22-alpine
RUN apk add --no-cache ca-certificates tzdata su-exec && addgroup -S -g 1001 blog && adduser -S -D -u 1001 -G blog blog
WORKDIR /app
COPY --from=build /out/blog-server /app/blog-server
COPY --from=renderer /src/frontend/renderer /app/renderer
COPY --from=theme /src/themes/default /app/themes/default
COPY config/config.yaml /app/config.default.yaml
COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN mkdir -p /app/content /app/data /app/media /app/config /app/generated /app/cache /app/backups /app/renderer \
 && chmod +x /app/entrypoint.sh \
 && chown -R blog:blog /app
ENV BLOG_SERVER_PORT=8080
EXPOSE 8080
VOLUME ["/app/content", "/app/data", "/app/media", "/app/config", "/app/generated", "/app/backups"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/blog-server", "--root", "/app"]
