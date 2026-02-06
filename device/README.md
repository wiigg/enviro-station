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

## Run

```bash
BACKEND_BASE_URL=http://localhost:8080 \
INGEST_API_KEY=dev-ingest-key \
python main.py
```

If the backend is unavailable, readings are queued locally and retried in batches.

## Auto-start on boot (systemd)

1. Open `sensor.service.template`.
2. Replace:
- `<<PATH_TO_DEVICE_PROGRAM>>`
- `<<WORKING_DIRECTORY>>`
- `<<USER>>`
3. Save as `/etc/systemd/system/sensor.service`.
4. Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable sensor.service
sudo systemctl start sensor.service
```

Check status:

```bash
sudo systemctl status sensor.service
```
