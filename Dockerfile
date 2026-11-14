# boson directory: SvelteKit UI (ui/) built with Node, embedded into the Go
# binary, shipped on distroless. Build stages run on the build host's
# platform (no QEMU); Go cross-compiles for the target.
FROM --platform=$BUILDPLATFORM node:24-alpine AS ui
WORKDIR /ui
COPY ui/ ./
# Go-only checkouts have no UI project yet; the binary then serves a stub page.
RUN if [ -f package.json ]; then npm ci --no-audit --no-fund && npm run build; else mkdir -p build; fi

FROM --platform=$BUILDPLATFORM golang:1.27 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/build ./ui/build
ARG TARGETOS TARGETARCH VERSION=dev
RUN mkdir -p /out/media && touch ui/build/.gitkeep && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/directory ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder --chown=65532:65532 /out/directory .
COPY --from=builder --chown=65532:65532 /out/media /media

ARG VERSION=dev
ENV VERSION=$VERSION ENV=production MEDIA_DIR=/media

# Local media (icons/banners) when no S3 bucket is configured: mount a volume.
VOLUME /media
# 8080: API + web UI (behind the reverse proxy). 8081: /metrics, /healthz (never publish).
EXPOSE 8080 8081
USER 65532:65532
ENTRYPOINT ["./directory"]
CMD ["serve"]
