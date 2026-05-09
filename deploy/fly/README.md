# Fly.io Cost-Minimized Deployment

These templates are tuned for minimum idle spend:

- one Machine per app
- `shared-cpu-1x` with `256mb` memory
- `auto_stop_machines = "stop"`
- `min_machines_running = 0`
- lazy backend database connection so live/status cold starts do not wake Neon
- dashboard same-origin proxy so `READ_API_KEY` is not built into browser JavaScript

## Files

- `backend.fly.toml`: backend API template
- `dashboard.fly.toml`: dashboard + Nginx reverse proxy template

Copy them to the ignored runtime locations before editing app names:

```bash
cp deploy/fly/backend.fly.toml backend/fly.toml
cp deploy/fly/dashboard.fly.toml dashboard-v2/fly.toml
```

## Backend

Set secrets on the API app:

```bash
fly secrets set -a your-envirostation-api \
  INGEST_API_KEY='replace-me' \
  READ_API_KEY='replace-me' \
  DATABASE_URL='postgres://...'
```

Deploy and force a single Machine:

```bash
cd backend
fly deploy -a your-envirostation-api --config fly.toml --remote-only
fly scale count 1 -a your-envirostation-api
cd ..
```

## Dashboard

Set the same read key on the dashboard app. Nginx injects it into proxied API
requests, including SSE, without exposing it in the built JavaScript bundle.

```bash
fly secrets set -a your-envirostation-dashboard READ_API_KEY='replace-me'
cd dashboard-v2
fly deploy -a your-envirostation-dashboard --config fly.toml --remote-only
fly scale count 1 -a your-envirostation-dashboard
cd ..
```

## Device Settings

For lowest Fly/Neon idle cost, keep:

```dotenv
DEVICE_LIVE_REQUIRE_SUBSCRIBER=true
DEVICE_LIVE_INTERVAL_SECONDS=1
DEVICE_LIVE_STATUS_INTERVAL_SECONDS=10
DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS=900
DEVICE_FLUSH_INTERVAL_SECONDS=1800
```

Increasing `DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS` reduces idle API wakeups further
at the cost of taking longer for the device to discover a newly opened dashboard.
