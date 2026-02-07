# Device Runtime

The device service reads Enviro+ sensors and sends readings to the backend ingest API.

## Required Environment

- `BACKEND_BASE_URL` (example: `http://localhost:8080`)
- `INGEST_API_KEY` (must match backend `INGEST_API_KEY`)

## Optional Environment

- `DEVICE_QUEUE_FILE` (default: `pending_readings.json`)
- `DEVICE_BATCH_SIZE` (default: `100`)
- `DEVICE_HTTP_TIMEOUT_SECONDS` (default: `5`)
- `DEVICE_MAX_PENDING` (default: `5000`)
- `DEVICE_TEMP_COMP_FACTOR` (default: `1.45`; lower value means more CPU heat compensation)
- `DEVICE_TEMP_OFFSET_C` (default: `0.6`; fixed subtraction after compensation)

## Bootstrap (one-time per device)

```bash
cd device
./install.sh
cp .env.local.example .env.local
```

`install.sh` installs required OS packages, configures Pi interfaces, installs `uv`,
creates `device/.venv`, and runs `uv sync`.

## Run manually

```bash
cd device
source .venv/bin/activate
python main.py
```

`main.py` loads configuration from `.env.local`.
If the backend is unavailable, readings are queued locally and retried in batches.

## Auto-start on boot (systemd)

```bash
cd device
./install_service.sh
```

Check logs:

```bash
journalctl -u sensor.service -f
```
