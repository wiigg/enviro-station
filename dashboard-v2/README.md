# Dashboard v2

Modern dashboard for Enviro Station operations.

## Scope

- Modern visual system and mobile-ready layout
- Backend history bootstrap from `GET /api/readings` (read API key)
- Realtime updates from `GET /api/stream` (SSE + read API key)
- AI insights from `GET /api/insights` (read API key)
- Connection state handling (`connecting`, `live`, `degraded`, `offline`)
- Ops feed panel for history + stream lifecycle events
- KPI cards and trend charts driven by live backend data
- Recharts-powered time-series panels with tooltip and responsive scaling

## Environment

```bash
VITE_BACKEND_URL=https://api.example.com
VITE_READ_API_KEY=<read-api-key>
```

If omitted in non-local deployments, the dashboard uses the current origin as backend base URL.
For local Vite dev (`localhost:5173`), it automatically targets `http://localhost:8080`.
If backend read auth is enabled (default), `VITE_READ_API_KEY` must match backend `READ_API_KEY`.
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
