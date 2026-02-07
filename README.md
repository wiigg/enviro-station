# Enviro Station

Enviro Station is an end-to-end air quality monitoring platform.

It collects environmental readings from edge devices, persists them in PostgreSQL, and exposes authenticated APIs for realtime and historical access.

## What It Includes

- Edge device runtime for sensor collection on Raspberry Pi
- Go backend for ingest, batch ingest, SSE streaming, recent reads, and AI insights
- PostgreSQL persistence via standard `DATABASE_URL` (works with local Postgres, Neon, or other managed providers)
- Modern React dashboard (`dashboard-v2`)

## Current Architecture

1. Device reads sensor values and posts to backend ingest endpoints.
2. Backend validates payloads, stores readings in Postgres, and emits stream events.
3. Clients consume current/recent data through backend APIs.

## Repository Structure

- `backend`: Go API service + Postgres migrations
- `device`: Sensor runtime and resilient transmitter (local queue + batch flush)
- `dashboard-v2`: Primary dashboard application

## Quick Start

### Backend + Postgres (Docker Compose)

```bash
cd backend
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

### Device Runtime

```bash
cd device
cp .env.local.example .env.local
uv sync
uv run main.py
```

## Cloud Deployment Checklist

- Backend:
Set `INGEST_API_KEY`, `READ_API_KEY`, `DATABASE_URL`, `CORS_ALLOW_ORIGIN`, and optional OpenAI insight variables.
- Device:
Set `BACKEND_BASE_URL` to your public API URL.
- Dashboard:
Set `VITE_BACKEND_URL` and `VITE_READ_API_KEY` at build time, or host dashboard and backend on the same origin with matching runtime config.
- Local development:
Create each service's `.env.local` from its `.env.local.example`.
- API base URL assumptions:
Frontend uses same-origin by default in non-local environments, and switches to `http://localhost:8080` only for local Vite dev.

## Backend API (Current)

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE, requires read API key)
- `GET /api/readings?limit=...` (requires read API key)
- `GET /api/insights?analysis_limit=...&limit=...` (requires read API key)
- `GET /health`
- `GET /ready`

## Status

- Provider-agnostic backend and device runtime
- Postgres-backed backend with migration support
- Dashboard v2 with live charts, AI insights, and ops feed
