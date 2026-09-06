# Build from the repository root so this image carries the version-pinned Rust
# verifier alongside the dedicated relay entrypoint.
FROM rust:1.85-bookworm AS core-dto-build
WORKDIR /build
COPY rust ./rust
RUN cargo build --locked --release --manifest-path rust/Cargo.toml -p muhan-core-dto --bin player_snapshot_v1_replay_verify

FROM node:22-bookworm-slim AS relay-build
WORKDIR /build/services/m4-file-snapshot-manifest-relay
COPY services/m4-file-snapshot-manifest-relay/package.json services/m4-file-snapshot-manifest-relay/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY services/m4-file-snapshot-manifest-relay ./
RUN npm run build

FROM node:22-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production
COPY services/m4-file-snapshot-manifest-relay/package.json services/m4-file-snapshot-manifest-relay/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund
COPY --from=relay-build /build/services/m4-file-snapshot-manifest-relay/dist ./dist
COPY --from=core-dto-build --chmod=0555 /build/rust/target/release/player_snapshot_v1_replay_verify /usr/local/libexec/muhan/player_snapshot_v1_replay_verify
ENV M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH=/usr/local/libexec/muhan/player_snapshot_v1_replay_verify
USER node
ENTRYPOINT ["node", "dist/player-snapshot-v1-manifest-first-cli.js"]
