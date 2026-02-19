# Dashboard v2

Modern React dashboard for Enviro Station.

## Environment

Use `dashboard-v2/.env.example` as the baseline.

Required variable:

```bash
VITE_BACKEND_URL=http://localhost:8080
```

If `VITE_BACKEND_URL` is unset:
- local Vite dev uses `http://localhost:8080`
- hosted builds default to same-origin (unless a hostname-specific fallback is defined in app code)

## Data loading behaviour

- History charts use time-range API queries (`from`/`to`/`max_points`) instead of large count-based fetches.
- `Live`, `1h`, and `24h` windows are prefetched and refreshed incrementally from SSE for fast switching.
- `7d` is loaded on demand when selected and refreshed on a longer cache TTL.
- Insights are cached in `sessionStorage` and revalidated in the background.

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
