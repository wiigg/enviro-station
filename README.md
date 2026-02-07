# Enviro Station

Enviro Station is an air quality monitoring platform with three services:

- `device`: Raspberry Pi runtime that reads sensors and ingests readings
- `backend`: Go API for ingest, storage, streaming, and AI insights
- `dashboard-v2`: React dashboard for live and historical visibility

## Architecture

1. Device reads Enviro+ sensors and posts to backend ingest endpoints.
2. Backend validates readings, stores data in Postgres, and emits SSE updates.
3. Dashboard reads history and subscribes to stream updates.

## Repository Layout

- `backend`: Go API + Postgres migrations
- `device`: Python runtime with queue-based retry
- `dashboard-v2`: Vite/React UI

## Environment Strategy

- Local development: use `.env.local` in each service.
- Cloud deployment: use `.env.cloud` as your deployment reference input.
- Committed templates:
  - `backend/.env.local.example`
  - `backend/.env.cloud.example`
  - `dashboard-v2/.env.local.example`
  - `dashboard-v2/.env.cloud.example`
  - `device/.env.local.example` (device is local/edge only)

## Quick Start

### Backend + Postgres (Docker Compose)

```bash
cd backend
cp .env.local.example .env.local
docker compose up --build
```

Backend runs on `http://localhost:8080`.

### Dashboard

```bash
cd dashboard-v2
cp .env.local.example .env.local
npm install
npm run dev
```

### Device

```bash
cd device
./install.sh
cp .env.local.example .env.local
source .venv/bin/activate
python main.py
```

## Backend API

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE)
- `GET /api/readings?limit=...`
- `GET /api/insights?analysis_limit=...&limit=...`
- `GET /health`
- `GET /ready`
