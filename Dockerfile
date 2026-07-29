# ---- Web (Astro) builder ----
FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS web-builder

WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# ---- Go builder ----
# Runs on the native build platform and cross-compiles to TARGETOS/TARGETARCH —
# QEMU emulation of the full toolchain (bun/go/upx) is flaky (Go runtime GC crashes
# under emulated atomics), so only the final `go build` targets the foreign arch.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go-builder
ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache upx ca-certificates tzdata

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /app/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -p=1 -ldflags="-s -w -X github.com/ilter-ai/ilter/internal/version.Version=$(cat VERSION)" -o ilter ./cmd/ilter/
RUN upx --best --lzma ilter

# ---- Distroless runtime ----
FROM scratch

COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-builder /app/ilter /ilter

EXPOSE 8181
EXPOSE 9191
ENTRYPOINT ["/ilter", "serve"]
