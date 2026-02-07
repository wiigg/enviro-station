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
