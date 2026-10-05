# syntax=docker/dockerfile:1
#
# Multi-stage production build for the baseline API.
#
#   builder : compiles a fully static binary
#   runtime : distroless image containing only the binary - no shell, no
#             package manager, no compilers
#
# The distroless runtime has no curl/wget, so the container HEALTHCHECK calls
# the binary's own `-health-check` flag instead.

# ---------------------------------------------------------------------------
# builder
# ---------------------------------------------------------------------------
# BUILDPLATFORM runs the toolchain natively (fast); TARGETARCH is the
# automatic BuildKit platform arg, so the binary matches the host/VM CPU.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /src

# Dependencies are copied first so the module download layer is cached
# independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

# CGO_ENABLED=0 produces a static binary that runs in a scratch-like image.
# -trimpath removes local filesystem paths; -s -w drop the symbol table and
# DWARF data (smaller image, no behavioural change).
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/migrate ./cmd/migrate

# ---------------------------------------------------------------------------
# runtime
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=builder /out/server /app/server
COPY --from=builder /out/migrate /app/migrate

# Distroless nonroot user is uid/gid 65532.
USER nonroot:nonroot

EXPOSE 8080

ENV APP_HOST=0.0.0.0 \
    APP_PORT=8080 \
    LOG_FORMAT=json \
    LOG_LEVEL=info

# The app polls its own /ready endpoint; no extra tooling needed in the image.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/app/server", "-health-check"]

ENTRYPOINT ["/app/server"]
