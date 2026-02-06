# Backend Ingest Service

Minimal Go service for Enviro Station ingestion, streaming, and recent reads.

## Endpoints

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE realtime stream)
- `GET /api/readings?limit=100`
- `GET /health`
- `GET /ready`

## Environment

- `PORT` (default: `8080`)
- `CORS_ALLOW_ORIGIN` (default: `*`)
- `INGEST_API_KEY` (required)
- `DATABASE_URL` (required, standard Postgres DSN)
- `PG_MAX_CONNS` (default: `10`)

## Run locally

```bash
INGEST_API_KEY='dev-ingest-key' \
DATABASE_URL='postgres://postgres:postgres@localhost:5432/envirostation?sslmode=disable' \
go run ./cmd/server
```

## Example ingest

```bash
curl -X POST http://localhost:8080/api/ingest \
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
curl -X POST http://localhost:8080/api/ingest/batch \
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
go run ./cmd/simulator \
  -url http://localhost:8080/api/ingest \
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
