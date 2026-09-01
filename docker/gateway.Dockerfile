# syntax=docker/dockerfile:1
# The gateway owns WSS, JWT validation, Telnet filtering, and browser-facing
# limits.  Its source is deliberately isolated from the C MUD image.
FROM --platform=linux/amd64 node:22-bookworm-slim AS build

WORKDIR /app

COPY services/gateway/package*.json ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

COPY services/gateway/ ./
RUN npm run build \
 && npm prune --omit=dev

FROM --platform=linux/amd64 node:22-bookworm-slim AS runtime

RUN groupadd --gid 10001 gateway \
 && useradd --uid 10001 --gid gateway --create-home --home-dir /app --shell /usr/sbin/nologin gateway

WORKDIR /app
ENV NODE_ENV=production
COPY --from=build --chown=gateway:gateway /app/package.json ./package.json
COPY --from=build --chown=gateway:gateway /app/node_modules ./node_modules
COPY --from=build --chown=gateway:gateway /app/dist ./dist

USER gateway:gateway
EXPOSE 8080/tcp
HEALTHCHECK --interval=10s --timeout=2s --start-period=5s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:' + (process.env.PORT || '8080') + '/healthz').then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"]
CMD ["node", "dist/index.js"]
