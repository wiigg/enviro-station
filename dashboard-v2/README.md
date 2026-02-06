# Dashboard v2

Phase 2 live integration for the Enviro Station dashboard rebuild.

## Scope

- Modern visual system and mobile-ready layout
- Backend history bootstrap from `GET /api/readings`
- Realtime updates from `GET /api/stream` (SSE)
- Connection state handling (`connecting`, `live`, `degraded`, `offline`)
- KPI cards and trend charts driven by live backend data
- Recharts-powered time-series panels with tooltip and responsive scaling

## Environment

```bash
VITE_BACKEND_URL=http://localhost:8080
```

## Run

```bash
npm install
npm run dev
```

## Build

```bash
npm run build
```
