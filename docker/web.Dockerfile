# syntax=docker/dockerfile:1
FROM --platform=linux/amd64 node:22-bookworm-slim AS build

WORKDIR /app
RUN corepack enable && corepack prepare pnpm@10.15.1 --activate

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json ./web/package.json
COPY services/gateway/package.json ./services/gateway/package.json
RUN pnpm install --frozen-lockfile --filter @muhan/web...

COPY web/ ./web/
RUN pnpm --filter @muhan/web build

FROM --platform=linux/amd64 node:22-bookworm-slim AS runtime

RUN groupadd --gid 10001 web \
 && useradd --uid 10001 --gid web --create-home --home-dir /app --shell /usr/sbin/nologin web

WORKDIR /app
ENV NODE_ENV=production \
    HOSTNAME=0.0.0.0 \
    PORT=3000

COPY --from=build --chown=web:web /app/web/.next/standalone ./
COPY --from=build --chown=web:web /app/web/.next/static ./web/.next/static

USER web:web
EXPOSE 3000/tcp
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:' + (process.env.PORT || '3000') + '/').then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"]
CMD ["node", "web/server.js"]
