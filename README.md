# Enviro Station

Enviro Station is an air quality monitoring platform with three services:

- `device`: Raspberry Pi runtime that reads sensors and ingests readings
- `backend`: Go API for ingest, storage, streaming, and insights
- `dashboard-v2`: React dashboard for live and historical visibility

## Architecture

1. Device reads Enviro+ sensors every second and publishes in-memory live readings every 30 seconds.
2. Device retains one durable sample per minute and flushes those samples to Postgres every 30 minutes.
3. Dashboard uses the live stream for realtime updates and device-scoped Postgres buckets for longer-range history.
4. If Postgres is down, the backend can still boot in live-only mode and retry durable storage later.
5. Particle readings carry `pm_available`; failed PMS5003 reads are stored as unavailable and excluded from charts and insights.
6. Optional backend-only outdoor context uses postcode-derived coordinates with Open-Meteo temperature, humidity, and modelled CAMS European air quality without exposing the configured location to the browser.
7. Insights make one shared ventilation decision from outdoor temperature, moisture, and particulate conditions, then keep each metric's explanation on its own card.

## Repository Layout

- `backend`: Go API + Postgres migrations
- `device`: Python runtime with queue-based retry
- `dashboard-v2`: Vite/React UI

## Environment Files

Each service tracks a single template file: `.env.example`.
Create a local runtime file by copying it to `.env`.

## Outdoor Location Privacy

`OUTDOOR_LOCATION` is sensitive and must exist only as a backend runtime secret,
such as a Fly secret. Keep its value out of tracked configuration, logs, model
prompts, API and browser data, test fixtures, and every commit; the tracked
`.env.example` entry must remain blank. CI enforces these repository guardrails
with `scripts/check-location-privacy.sh`.

The backend necessarily sends the normalised postcode only to Postcodes.io to
resolve it. The returned coordinates are rounded to two decimal places before
being sent to Open-Meteo for weather and air-quality lookups. Insights receive
the resulting environmental metrics, never the postcode or coordinates.

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

Fly templates live in `deploy/fly/`. They use one `shared-cpu-1x` 256MB Machine
per app, keep one Machine warm for reliable live telemetry and SSE, and use that
single Machine count as the scale-out cap. Deploy with `--ha=false`, keep
`fly scale count 1`, and keep the backend database connection lazy so live/status
traffic does not touch Neon until durable history or batch ingest needs Postgres.
