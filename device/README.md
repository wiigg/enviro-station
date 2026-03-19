# Device Runtime

The device service reads Enviro+ sensors, publishes live readings to the backend,
and flushes durable batches to Postgres on a timer.

## Required Environment

- `BACKEND_BASE_URL` (example: `http://localhost:8080`)
- `INGEST_API_KEY` (must match backend `INGEST_API_KEY`)

## Optional Environment

- `DEVICE_QUEUE_FILE` (default: `pending_readings.json`)
- `DEVICE_BATCH_SIZE` (default: `1000`)
- `DEVICE_FLUSH_INTERVAL_SECONDS` (default: `1800`)
- `DEVICE_HTTP_TIMEOUT_SECONDS` (default: `5`)
- `DEVICE_MAX_PENDING` (default: `5000`)
- `DEVICE_TEMP_COMP_FACTOR` (default: `1.45`; lower value means more CPU heat compensation)
- `DEVICE_TEMP_OFFSET_C` (default: `0.6`; fixed subtraction after compensation)

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
Each reading is sent to the backend live stream immediately.
Durable writes are queued locally and flushed to Postgres in timed batches.
If the backend is unavailable, queued readings are retried on the next flush.

## Auto-start on boot (systemd)

```bash
cd device
./install_service.sh
```

Check logs:

```bash
journalctl -u sensor.service -f
```
