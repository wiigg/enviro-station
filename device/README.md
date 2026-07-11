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
- `DEVICE_DURABLE_INTERVAL_SECONDS` (default: `60`; interval between locally queued durable samples)
- `DEVICE_FLUSH_INTERVAL_SECONDS` (default: `1800`; interval between durable batch uploads)
- `DEVICE_LIVE_INTERVAL_SECONDS` (default: `30`; set `0` to disable live posts)
- `DEVICE_LIVE_REQUIRE_SUBSCRIBER` (default: `false`; optionally only live-post when a dashboard stream is connected)
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
```

`install.sh` installs required OS packages, configures Pi interfaces, installs `uv`,
creates `device/.venv`, runs `uv sync`, and enables automatic system updates. It
creates `.env` from `.env.example` when no local environment file exists.

## Automatic OS maintenance

The versioned policy at `config/52envirostation-unattended-upgrades` is installed
to `/etc/apt/apt.conf.d/` during bootstrap. It retains Debian's standard daily
timers and additionally allows signed packages from Raspbian, Raspberry Pi, and
the Tailscale stable repository. Package caches are cleaned weekly, obsolete
kernels are removed, and a reboot occurs at 04:30 only when an update requires
one and no user is logged in.

Reapply the policy to an existing device without rerunning the full bootstrap:

```bash
cd device
./configure_auto_updates.sh
```

Verify the effective schedule and policy:

```bash
systemctl list-timers apt-daily.timer apt-daily-upgrade.timer --all
sudo unattended-upgrade --dry-run --verbose
```

Tailscale installation and Tailnet authentication remain device-specific. Once
its stable APT repository is configured, this policy keeps Tailscale updated.

## Run manually

```bash
cd device
uv run python main.py
```

`main.py` loads configuration from `.env`.
The sensor and display update every second, but transmission uses two independent
cadences. Live readings post every 30 seconds to the backend's in-memory stream;
they do not write to Postgres. One reading per minute is queued locally for
durable history, and those readings are uploaded in a batch every 30 minutes.
This produces about 2,880 small live requests, 1,440 durable rows, and 48 durable
batch uploads per day.

Subscriber gating is optional. If `DEVICE_LIVE_REQUIRE_SUBSCRIBER=true`, status
checks back off exponentially up to `DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS` while
no dashboard is connected. The default keeps gating disabled so a newly opened
dashboard always has a recent live reading.
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
