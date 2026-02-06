# Enviro Station

Enviro Station is an end-to-end air quality monitoring platform.

It collects environmental readings from edge devices, persists them in PostgreSQL, and provides a backend API for realtime and historical access. The project is currently migrating to a cleaner, provider-agnostic architecture centered on Docker-friendly services.

## What It Includes

- Edge device runtime for sensor collection on Raspberry Pi
- Go backend for ingest, batch ingest, SSE streaming, and recent readings
- PostgreSQL persistence via standard `DATABASE_URL` (works with local Postgres, Neon, or other managed providers)
- New dashboard rebuild (`dashboard-v2`) in progress

## Current Architecture

1. Device reads sensor values and posts to backend ingest endpoints.
2. Backend validates payloads, stores readings in Postgres, and emits stream events.
3. Clients consume current/recent data through backend APIs.

## Repository Structure

- `backend`:
Go API service (ingest, batch ingest, stream, readings, health/readiness)
- `device`:
Sensor runtime and resilient transmitter (local queue + batch flush)
- `dashboard`:
Legacy dashboard (kept for reference during migration)
- `dashboard-v2`:
New dashboard foundation (Phase 1)

## Quick Start

### Backend + Postgres (Docker Compose)

```bash
cd backend
docker compose up --build
```

Backend runs on `http://localhost:8080`.

### Device Runtime

```bash
cd device
cp .env.example .env
python3 main.py
```

### Dashboard v2 (Phase 1)

```bash
cd dashboard-v2
npm install
npm run dev
```

## Backend API (Current)

- `POST /api/ingest` (requires `X-API-Key`)
- `POST /api/ingest/batch` (requires `X-API-Key`)
- `GET /api/stream` (SSE)
- `GET /api/readings?limit=...`
- `GET /health`
- `GET /ready`

## Status

- Azure Functions stack removed from active runtime
- Device sender migrated off Azure IoT
- Postgres-backed backend in place with migration support
- Dashboard v2 started; data wiring and live views are next
