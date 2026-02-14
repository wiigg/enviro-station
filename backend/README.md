# Backend Ingest Service

Go service for Enviro Station ingest, storage, streaming, and AI insights.
Ingest endpoints require `INGEST_API_KEY`; read endpoints are public in this version.

## Endpoints

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE)
- `GET /api/readings?limit=100`
- `GET /api/insights?limit=3`
- `GET /api/ops/events?limit=30`
- `GET /health`
- `GET /ready`

## Environment

- `PORT` (default: `8080`)
- `CORS_ALLOW_ORIGIN` (default: `*`; set explicit origins for production)
- `INGEST_API_KEY` (required)
- `DATABASE_URL` (required)
- `PG_MAX_CONNS` (default: `10`)
- `TRUST_PROXY_HEADERS` (default: `false`)
- `OPS_DEVICE_OFFLINE_TIMEOUT` (default: `45s`)
- `OPS_MONITOR_INTERVAL` (default: `5s`)
- `OPENAI_API_KEY` (optional; enables `/api/insights`)
- `OPENAI_INSIGHTS_MODEL` (default: `gpt-5-mini`)
- `OPENAI_BASE_URL` (default: `https://api.openai.com/v1`)
- `OPENAI_INSIGHTS_MAX` (default: `3`; hard-capped at `3`)
- `OPENAI_INSIGHTS_ANALYSIS_LIMIT` (default: `900`)
- `OPENAI_INSIGHTS_REFRESH_INTERVAL` (default: `1h`)
- `OPENAI_INSIGHTS_EVENT_MIN_INTERVAL` (default: `10m`)
- `OPENAI_INSIGHTS_PM2_TRIGGER` (default: `8`)
- `OPENAI_INSIGHTS_PM10_TRIGGER` (default: `30`)
- `OPENAI_INSIGHTS_PM2_DELTA_TRIGGER` (default: `5`; upward jump threshold)
- `OPENAI_INSIGHTS_PM10_DELTA_TRIGGER` (default: `15`; upward jump threshold)
- `OPENAI_INSIGHTS_ANALYZE_TIMEOUT` (default: `15s`)
- `RETENTION_ENABLED` (default: `true`)
- `RETENTION_DAYS` (default: `60`)
- `RETENTION_BATCH_SIZE` (default: `5000`)
- `RETENTION_INTERVAL` (default: `24h`)

Use `backend/.env.example` as the baseline and export/set values in your runtime environment.

## Run

```bash
cd backend
cp .env.example .env
go run ./cmd/server
```

`cmd/server` auto-loads `.env` when present.

## Data retention

Raw readings retention is managed automatically by the backend process.
By default, readings older than 60 days are deleted in batches every 24 hours.

## Insights generation model

Insights are precomputed in the backend, not generated per request.
- Scheduled recompute at `OPENAI_INSIGHTS_REFRESH_INTERVAL`
- Event-triggered recompute on significant PM threshold crossings or jumps
- `/api/insights` returns the latest stored snapshot
- Latest snapshot is persisted in Postgres and restored on backend restart

## Docker Compose (backend + postgres)

```bash
cd backend
docker compose up --build
```

## Device Simulator (dev)

```bash
go run ./cmd/simulator \
  -url "http://localhost:8080/api/ingest" \
  -api-key dev-ingest-key \
  -interval 2s
```
