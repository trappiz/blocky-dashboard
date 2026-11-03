# syntax=docker/dockerfile:1

# Base images are pinned by digest (multi-arch index) so a re-pushed tag can't
# change what gets built; Dependabot/Renovate update tag and digest together.

# ---- build ----
# Runs on the build host's architecture and cross-compiles for the target,
# so multi-arch images build without QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/blocky-dashboard ./cmd/blocky-dashboard

# ---- runtime ----
# Distroless static: no shell or package manager, runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/blocky-dashboard /blocky-dashboard

# Listen on all interfaces inside the container. Mount a config file and set
# BLOCKY_DASHBOARD_CONFIG, or point at a single Blocky with
# BLOCKY_DASHBOARD_BLOCKY_URL.
ENV BLOCKY_DASHBOARD_LISTEN=:3001
EXPOSE 3001
USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/blocky-dashboard", "-healthcheck"]

ENTRYPOINT ["/blocky-dashboard"]
