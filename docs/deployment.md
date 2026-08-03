# ILTER Deployment Guide

> Production deployment options for ILTER AI Gateway: single binary, Docker, Docker Compose, and Kubernetes.

---

## Prerequisites

- **ILTER binary** — Build from source (`make build`) or download from GitHub Releases (~35-40MB, unstripped-UPX plain binary; the <20MB figure below is the Docker image only)
- **Configuration** — Via `ILTER_*` environment variables (see [`configuration.md`](configuration.md))
- **Redis** (optional) — Required for semantic cache and distributed rate limiting (fails open if unavailable)
- **Ollama** (optional) — Required for local semantic cache embeddings (fails open if unavailable)

---

## Network Ports Overview

| Port | Service | Protocol | Description |
|------|---------|----------|-------------|
| **8181** | Proxy API | HTTP/1.1, SSE | Main LLM Gateway (`/v1/chat/completions`, `/v1/messages`, `/v1/completions`, `/v1/embeddings`, `/v1/rerank`, `/v1/models`, `/admin/*`, `/mcp`, OAuth PKCE) |
| **9191** | Dashboard | HTTP/1.1, JSON | Embedded Web UI + Admin REST API (`/api/*`) |
| **9192** | Metrics | HTTP/1.1 | OpenTelemetry Prometheus scrape endpoint (`/metrics`) |

---

## Local / Development

### Quick Start

```bash
# Just run it — compiled-in defaults work out of the box
./ilter serve
```

### First-Time Setup

```bash
# Interactive wizard — configure providers, routing, feature flags
./ilter init

# Or seed demo data for dashboard development
./ilter init --demo

# Then start
./ilter serve
```

### Hot Reload

```bash
make dev
```

`make dev` runs in a dedicated terminal tab: Air (Go hot reload backend) and Vite (Astro frontend HMR) run concurrently.

---

## Docker

### Build

```bash
# Build using the root Dockerfile
docker build -t ilter .
```

### Multi-Stage Container Architecture

1. **Stage 1: Web Builder (`oven/bun:1-alpine`)** — Installs dependencies and builds Astro + React SPA into `web/dist`.
2. **Stage 2: Go Builder (`golang:1.26-alpine`)** — Compiles statically linked Go binary (`CGO_ENABLED=0`) and compresses with UPX (`upx --best --lzma`).
3. **Stage 3: Base Runtime (`scratch`)** — Minimal empty image containing only the compiled binary, CA certificates (`/etc/ssl/certs/ca-certificates.crt`), and timezone database (`/usr/share/zoneinfo`).

**Final image size: <20MB**.

### Run Container

```bash
docker run -d \
  --name ilter \
  -p 8181:8181 \
  -p 9191:9191 \
  -p 9192:9192 \
  -v $(pwd)/data:/app/data \
  ilter
```

---

## Docker Compose (Full Local Stack)

The `docker-compose.yaml` in project root spins up ILTER alongside optional services:

```bash
docker compose up -d
```

### Services

| Service | Image | Ports | Purpose | Optional? |
|---------|-------|-------|---------|-----------|
| **ilter** | Built from `./Dockerfile` | 8181, 9191, 9192 | Core Gateway, Dashboard, Metrics | Required |
| **redis** | `redis/redis-stack:latest` | 6379, 8001 | Semantic cache VSS + rate limiting | Optional (fails open) |
| **ollama** | `ollama/ollama:latest` | 11434 | Local vector embeddings (`nomic-embed-text`) | Optional (fails open) |

---

## Kubernetes Deployment

Since ILTER is a single stateless binary with embedded assets, deployment to Kubernetes is straightforward using standard Kubernetes manifests.

### Kubernetes Deployment & Service Manifest (`ilter-k8s.yaml`)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ilter-gateway
  labels:
    app: ilter
spec:
  replicas: 2
  selector:
    matchLabels:
      app: ilter
  template:
    metadata:
      labels:
        app: ilter
    spec:
      containers:
      - name: ilter
        image: ilter:latest
        imagePullPolicy: IfNotPresent
        env:
        - name: ILTER_SERVER_PORT
          value: "8181"
        - name: ILTER_DASHBOARD_PORT
          value: "9191"
        - name: ILTER_METRICS_LISTEN_ADDR
          value: ":9192"
        - name: ILTER_STORAGE_PATH
          value: "/app/data/ilter.db"
        ports:
        - containerPort: 8181
          name: proxy
        - containerPort: 9191
          name: dashboard
        - containerPort: 9192
          name: metrics
        livenessProbe:
          httpGet:
            path: /admin/health
            port: 8181
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /admin/health
            port: 8181
          initialDelaySeconds: 2
          periodSeconds: 5
        volumeMounts:
        - name: data-volume
          mountPath: /app/data
      volumes:
      - name: data-volume
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: ilter-service
spec:
  type: ClusterIP
  selector:
    app: ilter
  ports:
  - port: 8181
    targetPort: 8181
    name: proxy
  - port: 9191
    targetPort: 9191
    name: dashboard
  - port: 9192
    targetPort: 9192
    name: metrics
```

Deploy with:

```bash
kubectl apply -f ilter-k8s.yaml
```

---

## Monitoring & Prometheus Configuration

ILTER exposes OpenTelemetry metrics in Prometheus format at `http://<host>:9192/metrics`.

### Prometheus Scrape Configuration

Add the following job to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'ilter'
    scrape_interval: 15s
    static_configs:
      - targets: ['ilter-service:9192']
```

Key operational metrics exposed:
- `ilter_requests_total` — Request counters by provider, model, and status
- `ilter_request_duration_seconds` — Latency distribution histograms
- `ilter_token_usage_total` — Input and output tokens consumed
- `ilter_guardrail_violations_total` — Violations triggered by type and severity
- `ilter_mcp_tool_calls_total` — Tool call counts and latency

---

## Troubleshooting & Operations

### Debugging Empty `scratch` Base Containers

Because the final runtime container is built on an empty `scratch` image, shell utilities like `sh`, `bash`, `curl`, or `ls` are not present inside the container.

- **Kubernetes**: Use [Ephemeral Debug Containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/):
  ```bash
  kubectl debug -it pod/ilter-gateway-xxxx --image=busybox --target=ilter
  ```
- **Docker**: Inspect logs or copy binary/data to host:
  ```bash
  docker cp ilter:/app/data/ilter.db ./ilter.db
  ```

### Fail-Open Degradation

ILTER is designed for high availability. If Redis or Ollama become unreachable:
- **Redis offline**: Rate limiting and semantic caching degrade gracefully (fail open). Requests pass through to upstream providers without caching or distributed rate enforcement.
- **Ollama offline**: Semantic cache falls back to exact SHA256 prompt hash matching. Proxy requests continue normally.
