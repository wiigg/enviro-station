# Backend Ingest Service

Go service for Enviro Station ingest, storage, streaming, and AI insights.
Ingest endpoints require `INGEST_API_KEY`; read endpoints are public in this version.

## Endpoints

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE)
- `GET /api/readings?limit=100`
- `GET /api/insights?analysis_limit=360&limit=4`
- `GET /health`
- `GET /ready`

## Environment

- `PORT` (default: `8080`)
- `CORS_ALLOW_ORIGIN` (default: `*`; set explicit origins for production)
- `INGEST_API_KEY` (required)
- `DATABASE_URL` (required)
- `PG_MAX_CONNS` (default: `10`)
- `TRUST_PROXY_HEADERS` (default: `false`)
- `OPENAI_API_KEY` (optional; enables `/api/insights`)
- `OPENAI_INSIGHTS_MODEL` (default: `gpt-5-mini`)
- `OPENAI_BASE_URL` (default: `https://api.openai.com/v1`)
- `OPENAI_INSIGHTS_MAX` (default: `4`)
- `OPENAI_INSIGHTS_CACHE_SECONDS` (default/minimum: `30`)

## Local Run

```bash
cd backend
cp .env.local.example .env.local
go run ./cmd/server
```

`cmd/server` auto-loads `.env.local` when present.

## Cloud Run

Use `backend/.env.cloud.example` as your reference and set variables in your platform secret/env system.

## Docker Compose (backend + postgres)

```bash
cd backend
docker compose up --build
```

## Device simulator (dev)

```bash
go run ./cmd/simulator \
  -url "http://localhost:8080/api/ingest" \
  -api-key dev-ingest-key \
  -interval 2s
```
