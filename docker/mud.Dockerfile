# syntax=docker/dockerfile:1
# The game persists native C structs, so keep the build and runtime Linux/amd64.
FROM --platform=linux/amd64 gcc:14 AS build

WORKDIR /build

COPY src/ ./src/

# The default Makefile includes -DDEBUG.  That is intentional: main() then
# remains in the foreground, which is required for a container entrypoint.
RUN make -C src clean \
 && make -C src -j4 CC=gcc \
 && mkdir -p bin \
 && make -C src auth CC=gcc

FROM --platform=linux/amd64 debian:bookworm-slim AS runtime

RUN groupadd --gid 10001 muhan \
 && useradd --uid 10001 --gid muhan --create-home --home-dir /home/muhan --shell /usr/sbin/nologin muhan

# Keep immutable image assets separate from the mounted game volume.  The
# compose mud-init service copies this tree into an empty named volume.
WORKDIR /opt/muhan-seed
COPY --chown=muhan:muhan rooms/ ./rooms/
COPY --chown=muhan:muhan objmon/ ./objmon/
COPY --chown=muhan:muhan player/ ./player/
COPY --chown=muhan:muhan post/ ./post/
COPY --chown=muhan:muhan help/ ./help/
COPY --chown=muhan:muhan log/ ./log/
COPY --chown=muhan:muhan resources_utf8/ ./resources_utf8/
COPY --chown=muhan:muhan resources_manifest/ ./resources_manifest/
COPY --chown=muhan:muhan bin/ ./bin/
COPY --from=build --chown=muhan:muhan /build/src/frp.new ./bin/frp
COPY --from=build --chown=muhan:muhan /build/bin/auth ./bin/auth

COPY docker/mud-entrypoint.sh /usr/local/bin/mud-entrypoint
COPY docker/mud-healthcheck.sh /usr/local/bin/mud-healthcheck
RUN chmod 0555 /usr/local/bin/mud-entrypoint /usr/local/bin/mud-healthcheck \
 && chmod 0755 /opt/muhan-seed/bin/frp /opt/muhan-seed/bin/auth

ENV MUD_PORT=4000 \
    MUHAN_HOME=/home/muhan

WORKDIR /home/muhan
USER muhan:muhan

# This documents the private game protocol port.  Compose intentionally does
# not publish it to the host.
EXPOSE 4000/tcp

HEALTHCHECK --interval=10s --timeout=2s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/mud-healthcheck"]

ENTRYPOINT ["/usr/local/bin/mud-entrypoint"]
