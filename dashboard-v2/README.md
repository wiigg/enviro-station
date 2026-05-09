# Dashboard v2

Modern React dashboard for Enviro Station.

## Environment

Use `dashboard-v2/.env.example` as the baseline.

Required variable:

```bash
VITE_BACKEND_URL=http://localhost:8080
```

Optional variables:

```bash
VITE_READ_API_KEY=
VITE_DEVICE_ID=
```

`VITE_READ_API_KEY` is sent on read API fetches and appended to the SSE URL as
`read_key` so the realtime stream still works when backend `READ_API_KEY` is set.
`VITE_DEVICE_ID` scopes history, live-buffer reads, and the SSE stream to one device.
For production Fly deployments, prefer the same-origin Nginx proxy in
`deploy/fly/dashboard.fly.toml`; it injects `READ_API_KEY` server-side so the
read key is not embedded in browser JavaScript.

If `VITE_BACKEND_URL` is unset:
- local Vite dev uses `http://localhost:8080`
- hosted builds default to same-origin

## Data loading behaviour

- History charts use capped time-range API queries (`from`/`to`/`max_points`) backed by server-side device-scoped buckets.
- Client-side chart downsampling uses multi-metric LTTB-style selection so particulate and temperature spikes are less likely to disappear.
- `Live` and `1h` windows are prefetched and refreshed incrementally from SSE for realtime updates while the dashboard is open.
- `24h` and `7d` are loaded on demand when selected and refreshed on longer cache TTLs.
- Insights are cached in `sessionStorage` and revalidated in the background.
- Ops feed reads the backend live ops buffer and refreshes every 5 minutes to avoid keeping Postgres compute warm.

## Container reverse proxy

The production image serves static assets with Nginx and proxies `/api/*` to
`API_UPSTREAM` at runtime.

Example runtime env:

```bash
API_UPSTREAM=http://api.internal:8080
READ_API_KEY=shared-read-key
```

This keeps browser traffic same-origin while backend access stays server-side.

## Run

```bash
cd dashboard-v2
cp .env.example .env
npm install
npm run dev
```

## Build

```bash
npm run build
```
