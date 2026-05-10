# Fly.io Deployment

These templates keep the live dashboard reliable while still using small
Machines and lazy database access:

- one Machine per app; this is the effective scale-out cap
- `shared-cpu-1x` with `256mb` memory
- `auto_stop_machines = "off"`
- `min_machines_running = 1`
- deploys use `--ha=false` so Fly does not add spare Machines
- lazy backend database connection so live/status cold starts do not wake Neon
- dashboard same-origin proxy so `READ_API_KEY` is not built into browser JavaScript

Fly does not use a `max_machines_running` setting in these app configs. The
Machine count is the cap, so keep it at `1` and verify it after deploys.

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
fly deploy -a your-envirostation-api --config fly.toml --remote-only --ha=false
fly scale count 1 -a your-envirostation-api --max-per-region 1 --yes
fly scale show -a your-envirostation-api
cd ..
```

## Dashboard

Set the same read key on the dashboard app. Nginx injects it into proxied API
requests, including SSE, without exposing it in the built JavaScript bundle.

```bash
fly secrets set -a your-envirostation-dashboard READ_API_KEY='replace-me'
cd dashboard-v2
fly deploy -a your-envirostation-dashboard --config fly.toml --remote-only --ha=false
fly scale count 1 -a your-envirostation-dashboard --max-per-region 1 --yes
fly scale show -a your-envirostation-dashboard
cd ..
```

## Device Settings

For lower Neon write volume while keeping the dashboard responsive, keep:

```dotenv
DEVICE_LIVE_REQUIRE_SUBSCRIBER=true
DEVICE_LIVE_INTERVAL_SECONDS=1
DEVICE_LIVE_STATUS_INTERVAL_SECONDS=10
DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS=900
DEVICE_FLUSH_INTERVAL_SECONDS=1800
```

Increasing `DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS` reduces idle API traffic further
at the cost of taking longer for the device to discover a newly opened dashboard.
