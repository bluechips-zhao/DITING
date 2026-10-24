# syntax=docker/dockerfile:1
FROM node:22.23.1-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
ARG FRONTEND_OFFLINE=false
RUN --mount=type=cache,target=/root/.npm \
    npm_config_proxy="${HTTP_PROXY:-}" npm_config_https_proxy="${HTTPS_PROXY:-}" \
    npm ci --no-audit --no-fund --maxsockets=4 --prefer-offline --offline="${FRONTEND_OFFLINE}"
COPY web/ ./
ENV NEXT_PUBLIC_MOCK=0
RUN npm run build:static
FROM golang:1.26.3-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . ./
COPY --from=frontend /src/web/out /src/server/webui/dist
ARG VERSION=2.6.0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -tags embedui \
    -ldflags "-s -w -buildid= -X main.version=${VERSION}" -o /out/artex ./cmd/artex
FROM node:22.23.1-bookworm-slim AS node-runtime
FROM python:3.12-slim-bookworm
COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=node-runtime /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
    && ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && apt-get -o Acquire::Retries=3 update && apt-get -o Acquire::Retries=3 install -y --no-install-recommends \
       ca-certificates ripgrep curl wget vim git jq unzip dnsutils iputils-ping \
       netcat-openbsd inetutils-telnet whois nmap \
    && rm -rf /var/lib/apt/lists/*
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright
RUN --mount=type=cache,target=/root/.npm \
    npm_config_proxy="${HTTP_PROXY:-}" npm_config_https_proxy="${HTTPS_PROXY:-}" \
    npm install -g --maxsockets=4 --prefer-offline @playwright/mcp@0.0.83 @playwright/cli@0.1.22 playwright@1.64.0 \
    && playwright-cli --help && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend /out/artex /app/artex
COPY start.sh /app/start.sh
COPY docker/ /app/docker/
COPY skills/ /opt/diting/skills/
RUN chmod +x /app/artex /app/start.sh \
    && groupadd -g 10001 diting && useradd -u 10001 -g diting -M diting \
    && mkdir -p /var/lib/diting/runtime /var/lib/diting/data /var/lib/diting/skills /var/lib/diting/home \
    && chown -R 10001:10001 /var/lib/diting
ENV ARTEX_HOME=/var/lib/diting/runtime ARTEX_SKILL_DIR=/var/lib/diting/skills HOME=/var/lib/diting/home
USER 10001:10001
EXPOSE 8787 8788
ENTRYPOINT ["python", "/app/docker/entrypoint.py"]
