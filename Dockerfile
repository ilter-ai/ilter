# ---- Web (Astro) builder ----
FROM oven/bun:1-alpine AS web-builder

WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# ---- Go builder ----
FROM golang:1.26-alpine AS go-builder

RUN apk add --no-cache upx ca-certificates tzdata

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /app/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -p=1 -ldflags="-s -w" -o ilter ./cmd/ilter/
RUN upx --best --lzma ilter

# ---- Distroless runtime ----
FROM scratch

COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=go-builder /app/ilter /ilter

EXPOSE 8181
EXPOSE 9191
ENTRYPOINT ["/ilter", "serve"]
