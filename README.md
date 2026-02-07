# Enviro Station

Enviro Station is an end-to-end air quality monitoring platform.

It collects environmental readings from edge devices, persists them in PostgreSQL, and provides a backend API for realtime and historical access.

## What It Includes

- Edge device runtime for sensor collection on Raspberry Pi
- Go backend for ingest, batch ingest, SSE streaming, and recent readings
- PostgreSQL persistence via standard `DATABASE_URL` (works with local Postgres, Neon, or other managed providers)
- Modern React dashboard (`dashboard-v2`)

## Current Architecture

1. Device reads sensor values and posts to backend ingest endpoints.
2. Backend validates payloads, stores readings in Postgres, and emits stream events.
3. Clients consume current/recent data through backend APIs.

## Repository Structure

- `backend`:
Go API service (ingest, batch ingest, stream, readings, health/readiness)
- `device`:
Sensor runtime and resilient transmitter (local queue + batch flush)
- `dashboard-v2`:
Primary dashboard application

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

### Dashboard

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
- `GET /api/insights?analysis_limit=...&limit=...`
- `GET /health`
- `GET /ready`

## Status

- Provider-agnostic backend and device runtime
- Postgres-backed backend with migration support
- Dashboard v2 with live charts, AI insights, and ops feed
