# Device Runtime

The device service reads Enviro+ sensors, publishes live readings to the backend,
and flushes durable batches to Postgres on a timer.

## Required Environment

- `BACKEND_BASE_URL` (example: `http://localhost:8080`)
- `INGEST_API_KEY` (must match backend `INGEST_API_KEY`)

## Optional Environment

- `DEVICE_QUEUE_FILE` (default: `pending_readings.json`)
- `DEVICE_READ_INTERVAL_SECONDS` (default: `1`)
- `DEVICE_BATCH_SIZE` (default: `1000`)
- `DEVICE_FLUSH_INTERVAL_SECONDS` (default: `60`)
- `DEVICE_LIVE_INTERVAL_SECONDS` (default: `1`; set `0` to disable live posts)
- `DEVICE_LIVE_REQUIRE_SUBSCRIBER` (default: `true`; only live-post when a dashboard stream is connected)
- `DEVICE_LIVE_STATUS_INTERVAL_SECONDS` (default: `10`; active/minimum subscriber check interval)
- `DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS` (default: `900`; max idle subscriber-check backoff)
- `DEVICE_HTTP_TIMEOUT_SECONDS` (default: `5`)
- `DEVICE_MAX_PENDING` (default: `5000`)
- `DEVICE_TEMP_COMP_FACTOR` (default: `1.45`; lower value means more CPU heat compensation)
- `DEVICE_TEMP_OFFSET_C` (default: `0.6`; fixed subtraction after compensation)
- `DEVICE_DISPLAY_FONT_SIZE` (default: `11`; compact dark metrics display)
- `DEVICE_DISPLAY_BACKLIGHT` (default: `0.35`; used when the display driver supports dimming)
- `DEVICE_WIFI_CHECK_INTERVAL_SECONDS` (default: `30`)

## Bootstrap (one-time per device)

```bash
cd device
./install.sh
cp .env.example .env
```

`install.sh` installs required OS packages, configures Pi interfaces, installs `uv`,
creates `device/.venv`, and runs `uv sync`.

## Run manually

```bash
cd device
uv run python main.py
```

`main.py` loads configuration from `.env`.
Each reading is queued locally. Durable writes flush every 60 seconds by default
so newly opened dashboards do not wait long for recent persisted history. Live
updates are rate-limited separately and, by default, are only posted while a
dashboard stream is connected.
When no dashboard is connected, subscriber checks back off exponentially up to
`DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS` so Fly can stop between idle wakeups.
When a dashboard stream is detected, live posts use `DEVICE_LIVE_INTERVAL_SECONDS`
for realtime updates.
Readings include the Raspberry Pi serial number as `device_id` so backend batch
retries are idempotent per device.
Durable writes are queued locally and flushed to Postgres in timed batches.
If the backend is unavailable, queued readings are retried on the next flush.
The device display uses a black background for normal readings and dark red only
for Wi-Fi or particulate alerts. Wi-Fi status and serial text are cached so the
display refresh does not shell out every second.

## Auto-start on boot (systemd)

```bash
cd device
./install_service.sh
```

Check logs:

```bash
journalctl -u sensor.service -f
```
