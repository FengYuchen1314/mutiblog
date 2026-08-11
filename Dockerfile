# syntax=docker/dockerfile:1.7
FROM node:24-bookworm AS web-build
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY ui/console/package.json ui/console/package.json
COPY apps/renderer/package.json apps/renderer/package.json
COPY packages/markdown/package.json packages/markdown/package.json
COPY themes/earth/package.json themes/earth/package.json
RUN pnpm install --frozen-lockfile
COPY ui ./ui
COPY apps ./apps
COPY packages ./packages
COPY themes ./themes
RUN pnpm build

FROM golang:1.25-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/mutiblog ./cmd/mutiblog

FROM node:24-bookworm-slim AS runtime
LABEL org.opencontainers.image.source="https://github.com/FengYuchen1314/mutiblog"
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
ENV MUTIBLOG_ADDRESS=:8080 \
    MUTIBLOG_DATA_DIR=/var/lib/mutiblog \
    MUTIBLOG_CONSOLE_DIR=/app/console \
    MUTIBLOG_RENDERER_CLI=/app/renderer/cli.mjs
COPY --from=go-build /out/mutiblog /usr/local/bin/mutiblog
COPY --from=web-build /src/ui/console/dist /app/console
COPY --from=web-build /src/apps/renderer/dist /app/renderer
COPY --from=web-build /src/themes/earth/dist /app/themes/earth
RUN mkdir -p /var/lib/mutiblog && chown -R node:node /var/lib/mutiblog /app
USER node
EXPOSE 8080
VOLUME ["/var/lib/mutiblog"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["node", "-e", "fetch('http://127.0.0.1:8080/health/live').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"]
ENTRYPOINT ["/usr/local/bin/mutiblog"]
