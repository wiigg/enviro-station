# Enviro Station

Enviro Station is an air quality monitoring platform with three services:

- `device`: Raspberry Pi runtime that reads sensors and ingests readings
- `backend`: Go API for ingest, storage, streaming, and AI insights
- `dashboard-v2`: React dashboard for live and historical visibility

## Architecture

1. Device reads Enviro+ sensors and publishes live readings to the backend stream while a dashboard is connected.
2. Device batches durable uploads so the backend writes to Postgres less often.
3. Dashboard uses the live stream for realtime updates and device-scoped Postgres buckets for longer-range history.
4. If Postgres is down, the backend can still boot in live-only mode and retry durable storage later.

## Repository Layout

- `backend`: Go API + Postgres migrations
- `device`: Python runtime with queue-based retry
- `dashboard-v2`: Vite/React UI

## Environment Files

Each service tracks a single template file: `.env.example`.
Create a local runtime file by copying it to `.env`.

## Quick Start

### Backend + Postgres (Docker Compose)

```bash
cd backend
cp .env.example .env
docker compose up --build
```

Backend runs on `http://localhost:8080`.

### Dashboard

```bash
cd dashboard-v2
cp .env.example .env
npm install
npm run dev
```

### Device

```bash
cd device
./install.sh
cp .env.example .env
uv run python main.py
```

## Backend API

- `POST /api/live` (requires `X-API-Key`; live stream only, no Postgres write)
- `GET /api/live/status` (requires `X-API-Key`; optional `device_id`)
- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE live stream)
- `GET /api/readings?limit=...`
- `GET /api/readings?from=...&to=...&max_points=...`
- `GET /api/readings?limit=...&source=live`
- `GET /api/insights?limit=...`
- `GET /api/ops/events?limit=...`
- `GET /api/ops/events?limit=...&source=live`
- `GET /health`
- `GET /ready`

## Fly Deployment

Cost-minimized Fly templates live in `deploy/fly/`.
They use one `shared-cpu-1x` 256MB Machine per app, `min_machines_running=0`,
autostop, and lazy backend database connection so live/status cold starts do not
touch Neon until durable history or batch ingest needs Postgres.
