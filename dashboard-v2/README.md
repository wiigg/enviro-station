# Dashboard v2

Modern dashboard for Enviro Station operations.

## Scope

- Modern visual system and mobile-ready layout
- Backend history bootstrap from `GET /api/readings`
- Realtime updates from `GET /api/stream` (SSE)
- AI insights from `GET /api/insights`
- Connection state handling (`connecting`, `live`, `degraded`, `offline`)
- Ops feed panel for history + stream lifecycle events
- KPI cards and trend charts driven by live backend data
- Recharts-powered time-series panels with tooltip and responsive scaling

## Environment

```bash
VITE_BACKEND_URL=https://api.example.com
```

If omitted in non-local deployments, the dashboard uses the current origin as backend base URL.
For local Vite dev (`localhost:5173`), it automatically targets `http://localhost:8080`.
For hosted deployments, set `VITE_BACKEND_URL` explicitly to avoid accidental same-origin API calls.
For local development, create `.env.local` from the example:

```bash
cp .env.local.example .env.local
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
