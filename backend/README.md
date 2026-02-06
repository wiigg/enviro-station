# Backend Ingest Service

Minimal Go service for ingesting Enviro Station sensor data.

## Endpoints

- `POST /api/ingest`
- `GET /health`
- `GET /ready`
- `GET /api/readings?limit=100`

## Environment

- `PORT` (default: `8080`)
- `CORS_ALLOW_ORIGIN` (default: `*`)
- `DATABASE_URL` (required, standard Postgres DSN)
- `PG_MAX_CONNS` (default: `10`)

Example `DATABASE_URL` values:

- Local container: `postgres://postgres:postgres@localhost:5432/envirostation?sslmode=disable`
- Neon/managed Postgres: use provider DSN as-is

## Run locally

```bash
DATABASE_URL='postgres://postgres:postgres@localhost:5432/envirostation?sslmode=disable' go run ./cmd/server
```

## Example ingest

```bash
curl -X POST http://localhost:8080/api/ingest \
  -H 'content-type: application/json' \
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

## Docker

```bash
docker build -t enviro-ingest ./backend
docker run --rm -p 8080:8080 \
  -e DATABASE_URL='postgres://postgres:postgres@host.docker.internal:5432/envirostation?sslmode=disable' \
  enviro-ingest
```
