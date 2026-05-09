# Backend Ingest Service

Go service for Enviro Station ingest, storage, streaming, and AI insights.
Ingest endpoints require `INGEST_API_KEY`; read endpoints are optionally protected
with `READ_API_KEY` and rate-limited by client IP by default.

## Endpoints

- `POST /api/live` (requires `X-API-Key`; updates the live stream without writing Postgres)
- `GET /api/live/status` (requires `X-API-Key`; optional `device_id`; returns live stream subscriber count)
- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`; durable batch write to Postgres)
- `GET /api/stream` (SSE; optional `device_id`)
- `GET /api/readings?limit=100`
- `GET /api/readings?from=...&to=...&max_points=...`
- `GET /api/readings?limit=100&source=live`
- `GET /api/insights?limit=3`
- `GET /api/ops/events?limit=30`
- `GET /api/ops/events?limit=30&source=live`
- `GET /health`
- `GET /ready`

### Readings query modes

`GET /api/readings` supports two query modes:
- `limit` mode: latest N rows, e.g. `?limit=100`
- `range` mode: explicit time window with bounded payload, e.g.
  `?from=1738886400000&to=1738972800000&max_points=1440`
- `live` mode: in-memory live buffer, e.g. `?limit=900&source=live`
- optional `device_id` filters live buffer, SSE stream, and range results

Range mode notes:
- `from` and `to` must be provided together
- timestamps may be unix seconds or milliseconds
- `from` must be less than `to`
- `max_points` must be between `1` and `2500` (default `1000`)
- range results are time-bucketed aggregates for one device, not raw row samples
- if `device_id` is omitted, range mode uses the latest device found in the requested window
- particulate buckets return averages in `pm1`/`pm2`/`pm10` and peaks in `pm1_max`/`pm2_max`/`pm10_max`
- comfort and gas buckets use averages

## Environment

- `PORT` (default: `8080`)
- `CORS_ALLOW_ORIGIN` (default: `*`; set explicit origins for production)
- `INGEST_API_KEY` (required)
- `READ_API_KEY` (optional; when set, read endpoints require `X-Read-API-Key`, `X-API-Key`, `read_key`, or `read_api_key` cookie)
- `READ_RATE_LIMIT_REQUESTS` (default: `60`; set `0` to disable read rate limiting)
- `READ_RATE_LIMIT_WINDOW` (default: `1m`)
- `DATABASE_URL` (required for durable history and insights persistence)
- `DATABASE_CONNECT_ON_START` (default: `true`; set `false` on Fly to keep live/status cold starts from waking Neon)
- `DATABASE_RECONNECT_INTERVAL` (default: `30s`)
- `PG_MAX_CONNS` (default: `10`)
- `OPS_DEVICE_OFFLINE_TIMEOUT` (default: `45s`)
- `OPS_MONITOR_INTERVAL` (default: `5s`)
- `TRUST_PROXY_HEADERS` (default: `false`)
- `LIVE_BUFFER_LIMIT` (default: `3600`)
- `OPENAI_API_KEY` (optional; enables `/api/insights`)
- `OPENAI_INSIGHTS_MODEL` (default: `gpt-5-mini`)
- `OPENAI_BASE_URL` (default: `https://api.openai.com/v1`)
- `OPENAI_INSIGHTS_MAX` (default: `3`; hard-capped at `3`)
- `OPENAI_INSIGHTS_ANALYSIS_LIMIT` (default: `900`)
- `OPENAI_INSIGHTS_REFRESH_INTERVAL` (default: `1h`)
- `OPENAI_INSIGHTS_EVENT_MIN_INTERVAL` (default: `10m`)
- `OPENAI_INSIGHTS_PM2_TRIGGER` (default: `8`)
- `OPENAI_INSIGHTS_PM10_TRIGGER` (default: `30`)
- `OPENAI_INSIGHTS_PM2_DELTA_TRIGGER` (default: `5`; 10 minute material-change threshold)
- `OPENAI_INSIGHTS_PM10_DELTA_TRIGGER` (default: `15`; 10 minute material-change threshold)
- `OPENAI_INSIGHTS_HUMIDITY_LOW_TRIGGER` (default: `40`)
- `OPENAI_INSIGHTS_HUMIDITY_HIGH_TRIGGER` (default: `60`)
- `OPENAI_INSIGHTS_HUMIDITY_DELTA_TRIGGER` (default: `8`; 10 minute material-change threshold)
- `OPENAI_INSIGHTS_TEMPERATURE_LOW_TRIGGER` (default: `18`)
- `OPENAI_INSIGHTS_TEMPERATURE_HIGH_TRIGGER` (default: `26`)
- `OPENAI_INSIGHTS_TEMPERATURE_DELTA_TRIGGER` (default: `1.5`; 10 minute material-change threshold)
- `OPENAI_INSIGHTS_ANALYZE_TIMEOUT` (default: `15s`)
- `RETENTION_ENABLED` (default: `true`)
- `RETENTION_RUN_ON_START` (default: `true`; set `false` when database connection is lazy)
- `RETENTION_DAYS` (default: `14`)
- `RETENTION_BATCH_SIZE` (default: `5000`)
- `RETENTION_INTERVAL` (default: `24h`)

Use `backend/.env.example` as the baseline and export/set values in your runtime environment.
For browser dashboards, the Vite app can send `VITE_READ_API_KEY`; SSE uses the
`read_key` query parameter because browsers cannot attach custom EventSource headers.
For public deployments, prefer a same-origin proxy or cookie flow so the read key is
not embedded in the JavaScript bundle.

## Run

```bash
cd backend
cp .env.example .env
go run ./cmd/server
```

`cmd/server` auto-loads `.env` when present.

## Fly

Use `deploy/fly/backend.fly.toml` as the cost-minimized Fly template. It keeps
`min_machines_running=0`, uses the smallest shared Machine size, reduces Neon
connection pressure, and sets `DATABASE_CONNECT_ON_START=false`.

## Data retention

Raw readings retention is managed automatically by the backend process.
By default, readings older than 14 days are deleted in batches every 24 hours.
For scale-to-zero Fly deployments, set `DATABASE_CONNECT_ON_START=false` and
`RETENTION_RUN_ON_START=false`; retention then runs only if the Machine remains
up for the configured interval after a durable database access.

## Live vs durable ingestion

- `POST /api/live` updates the in-memory live buffer and SSE stream immediately.
- `POST /api/ingest` falls back to live-only acceptance when Postgres is unavailable.
- `POST /api/ingest/batch` is intended for delayed durable writes to Postgres.
- `GET /api/readings?source=live` reads from the in-memory live buffer without touching Postgres.
- `GET /api/ops/events?source=live` reads from the in-memory ops buffer without touching Postgres.
- `GET /api/readings` reads persisted history from Postgres.
- Unfiltered range queries merge legacy `default` rows with the latest device id for continuity after device id rollout.
- If Postgres is unavailable at boot, the API starts in live-only mode and retries the database in the background.
- Durable writes are idempotent by `(device_id, timestamp)`; legacy readings without `device_id` use `default`.
- The `0004` migration creates the unique device/timestamp index concurrently so deploys avoid holding a long schema transaction.

## Insights generation model

Insights are precomputed in the backend, not generated per request.
- Scheduled recompute at `OPENAI_INSIGHTS_REFRESH_INTERVAL`
- Event-triggered recompute on significant threshold crossings or material changes
- Live readings trigger recompute checks and are used for analysis when newer than durable history
- Live-triggered recomputes avoid durable reads so realtime dashboards do not wake Neon for AI copy alone
- `/api/insights` returns the latest stored snapshot
- Durable-analysis snapshots are persisted in Postgres and restored on backend restart
- Live-only event snapshots stay in memory until the next durable recompute

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
