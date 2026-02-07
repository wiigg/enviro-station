# Backend Ingest Service

Minimal Go service for Enviro Station ingestion, streaming, and recent reads.

## Endpoints

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE realtime stream)
- `GET /api/readings?limit=100`
- `GET /api/insights?analysis_limit=360&limit=4` (AI-generated insights)
- `GET /health`
- `GET /ready`

## Environment

- `PORT` (default: `8080`)
- `CORS_ALLOW_ORIGIN` (default: `*`; set to exact origin or comma-separated origin list in production)
- `INGEST_API_KEY` (required)
- `DATABASE_URL` (required, standard Postgres DSN)
- `PG_MAX_CONNS` (default: `10`)
- `OPENAI_API_KEY` (optional, enables `/api/insights`)
- `OPENAI_INSIGHTS_MODEL` (default: `gpt-5-mini`)
- `OPENAI_BASE_URL` (default: `https://api.openai.com/v1`)
- `OPENAI_INSIGHTS_MAX` (default: `4`)
- `OPENAI_INSIGHTS_CACHE_SECONDS` (default/minimum: `30`)

## Run locally

```bash
cd backend
cp .env.local.example .env.local
go run ./cmd/server
```

`cmd/server` auto-loads `.env.local` (and `.env` as fallback) when present.
Environment variables already set in your shell take precedence.

## Cloud configuration

```bash
INGEST_API_KEY='<secure-random-key>' \
CORS_ALLOW_ORIGIN='https://dashboard.example.com' \
DATABASE_URL='postgres://user:pass@db.example.com:5432/envirostation?sslmode=require' \
OPENAI_API_KEY='<optional>' \
go run ./cmd/server
```

## Example ingest

```bash
BACKEND_URL='https://api.example.com'

curl -X POST "$BACKEND_URL/api/ingest" \
  -H 'content-type: application/json' \
  -H 'x-api-key: dev-ingest-key' \
  -d '{
    "timestamp": "1738886400",
    "temperature": "22.3",
    "pressure": "101325",
    "humidity": "39.9",
    "oxidised": "1.2",
    "reduced": "1.0",
    "nh3": "0.8",
    "pm1": "2",
    "pm2": "3",
    "pm10": "4"
  }'
```

## Example batch ingest

```bash
BACKEND_URL='https://api.example.com'

curl -X POST "$BACKEND_URL/api/ingest/batch" \
  -H 'content-type: application/json' \
  -H 'x-api-key: dev-ingest-key' \
  -d '[
    {"timestamp":"1738886400","temperature":"22.3","pressure":"101325","humidity":"39.9","oxidised":"1.2","reduced":"1.0","nh3":"0.8","pm1":"2","pm2":"3","pm10":"4"},
    {"timestamp":"1738886401","temperature":"22.4","pressure":"101320","humidity":"40.1","oxidised":"1.1","reduced":"1.1","nh3":"0.7","pm1":"3","pm2":"4","pm10":"5"}
  ]'
```

## Device simulator (dev)

Generate synthetic readings and post them to ingest:

```bash
BACKEND_URL='https://api.example.com'

go run ./cmd/simulator \
  -url "$BACKEND_URL/api/ingest" \
  -api-key dev-ingest-key \
  -interval 2s
```

Useful flags:

- `-count 120` to emit a fixed number of readings then exit
- `-seed 42` to replay deterministic synthetic data
- `-jitter 1s` to vary send timing and mimic real device cadence

## Docker

```bash
docker build -t enviro-ingest ./backend
docker run --rm -p 8080:8080 \
  -e INGEST_API_KEY='dev-ingest-key' \
  -e DATABASE_URL='postgres://postgres:postgres@host.docker.internal:5432/envirostation?sslmode=disable' \
  enviro-ingest
```

## Docker Compose (backend + postgres)

From the `backend` directory:

```bash
docker compose up --build
```

This starts:

- Postgres on `localhost:5432`
- Backend on `http://localhost:8080`

The backend runs DB migrations from `internal/server/migrations/` on startup.

## AI insights endpoint

When `OPENAI_API_KEY` is set, `/api/insights` analyzes recent readings and returns
actionable insights (alerts, trend insights, and tips) with severity (`critical`, `warn`, `info`).
Insights responses are rate-limited per client and model calls are cached to reduce abuse/cost.

Example:

```bash
BACKEND_URL='https://api.example.com'
curl "$BACKEND_URL/api/insights?analysis_limit=720&limit=4"
```
