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
- hosted builds use same-origin

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
