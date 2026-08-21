# Zero dependencies either side of the build: the module has no requires, so there is
# nothing to download and the build works offline.
FROM golang:1.26-alpine AS build

WORKDIR /src
# The tag this image is built from, shown in the page header so what is deployed is readable
# without opening a shell. Compose passes it; by hand it stays "local".
ARG VERSION=local
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Static binary: the runtime image has no libc to link against.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.buildVersion=${VERSION}" -o /out/fantasy ./cmd/fantasy

FROM alpine:3.21

WORKDIR /app
COPY --from=build /out/fantasy /usr/local/bin/fantasy
# The page's CSS, JS and HTML pieces are read at render time.
COPY assets/ ./assets/
COPY README.md ./

# The session, cache, settings and report all live here — mount it to persist them.
# Kept outside /app so it never collides with the source tree.
VOLUME /data

ENV FANTASY_LOG_JSON=1 \
    FANTASY_DATA_DIR=/data \
    FANTASY_ASSETS=/app/assets

EXPOSE 8000

# wget is in busybox, so the check needs nothing installed.
HEALTHCHECK --interval=60s --timeout=10s --start-period=180s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8000/healthz >/dev/null || exit 1

ENTRYPOINT ["fantasy"]
CMD ["serve", "--port", "8000", "--interval", "3600"]
