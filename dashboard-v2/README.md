# Dashboard v2

Modern React dashboard for Enviro Station.

## Environment

- Local dev: copy `.env.local.example` to `.env.local`
- Cloud deploy: use `.env.cloud.example` as your platform env reference

Required variable:

```bash
VITE_BACKEND_URL=https://api.example.com
```

If `VITE_BACKEND_URL` is unset:
- local Vite dev uses `http://localhost:8080`
- hosted builds use same-origin

## Run

```bash
cd dashboard-v2
cp .env.local.example .env.local
npm install
npm run dev
```

## Build

```bash
npm run build
```
