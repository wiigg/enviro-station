import json
import logging
import os
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


class BackendTransmitter:
    def __init__(
        self,
        base_url,
        api_key,
        queue_file="pending_readings.json",
        batch_size=1000,
        timeout_seconds=5,
        max_pending=5000,
        flush_interval_seconds=60,
        live_interval_seconds=1,
        live_require_subscriber=True,
        live_status_interval_seconds=10,
        live_status_idle_max_seconds=900,
        device_id="default",
    ):
        if not base_url:
            raise ValueError("BACKEND_BASE_URL is required")
        if not api_key:
            raise ValueError("INGEST_API_KEY is required")

        self.api_key = api_key
        self.device_id = str(device_id or "default")
        base_url = base_url.rstrip("/")
        self.live_url = base_url + "/api/live"
        self.live_status_url = (
            base_url
            + "/api/live/status?device_id="
            + urllib.parse.quote(self.device_id, safe="")
        )
        self.batch_url = base_url + "/api/ingest/batch"
        self.queue_file = queue_file
        self.batch_size = max(1, batch_size)
        self.timeout_seconds = timeout_seconds
        self.max_pending = max(1, max_pending)
        self.flush_interval_seconds = max(1, flush_interval_seconds)
        self.live_interval_seconds = max(0, live_interval_seconds)
        self.live_require_subscriber = live_require_subscriber
        self.live_status_interval_seconds = max(1, live_status_interval_seconds)
        self.live_status_idle_max_seconds = max(
            self.live_status_interval_seconds, live_status_idle_max_seconds
        )
        self.next_live_status_interval_seconds = self.live_status_interval_seconds
        self.pending = self._load_pending()
        self.last_flush_at = time.monotonic()
        self.last_live_at = 0
        self.last_live_status_at = 0
        self.live_subscribers = not live_require_subscriber
        self.live_status_checked = False

    def send(self, reading):
        reading = self._with_device_id(reading)
        self.pending.append(reading)
        if len(self.pending) > self.max_pending:
            drop_count = len(self.pending) - self.max_pending
            logging.warning(
                "Pending queue exceeded limit; dropping %s oldest readings", drop_count
            )
            self.pending = self.pending[drop_count:]

        self._persist_pending()
        live_ok = True
        if self._live_due():
            live_ok = self._send_live_if_enabled(reading)
        if self._flush_due():
            return self.flush()
        return live_ok

    def flush(self):
        if not self.pending:
            self.last_flush_at = time.monotonic()
            return True

        sent = 0
        while sent < len(self.pending):
            chunk = self.pending[sent : sent + self.batch_size]
            if not self._post_json(self.batch_url, chunk):
                break
            sent += len(chunk)

        if sent > 0:
            self.pending = self.pending[sent:]
            self._persist_pending()

        flushed_all = len(self.pending) == 0
        if flushed_all:
            self.last_flush_at = time.monotonic()
        return flushed_all

    def _flush_due(self):
        return time.monotonic() - self.last_flush_at >= self.flush_interval_seconds

    def _live_due(self):
        if self.live_interval_seconds == 0:
            return False
        return time.monotonic() - self.last_live_at >= self.live_interval_seconds

    def _send_live_if_enabled(self, reading):
        if not self._live_allowed():
            self.last_live_at = time.monotonic()
            return True
        self.last_live_at = time.monotonic()
        return self._post_json(self.live_url, reading)

    def _live_allowed(self):
        if not self.live_require_subscriber:
            return True
        if self._live_status_due():
            self._refresh_live_status()
        return self.live_subscribers

    def _live_status_due(self):
        if not self.live_status_checked:
            return True
        return (
            time.monotonic() - self.last_live_status_at
            >= self.next_live_status_interval_seconds
        )

    def _refresh_live_status(self):
        self.live_status_checked = True
        self.last_live_status_at = time.monotonic()
        payload = self._get_json(self.live_status_url)
        if payload is None:
            self.live_subscribers = False
            self._back_off_live_status_checks()
            return
        subscriber_count = int(payload.get("subscriber_count", 0))
        self.live_subscribers = subscriber_count > 0
        if self.live_subscribers:
            self.next_live_status_interval_seconds = self.live_status_interval_seconds
        else:
            self._back_off_live_status_checks()

    def _back_off_live_status_checks(self):
        self.next_live_status_interval_seconds = min(
            self.live_status_idle_max_seconds,
            max(
                self.live_status_interval_seconds,
                self.next_live_status_interval_seconds * 2,
            ),
        )

    def _with_device_id(self, reading):
        output = dict(reading)
        if not output.get("device_id"):
            output["device_id"] = self.device_id
        return output

    def _get_json(self, url):
        request = urllib.request.Request(
            url,
            method="GET",
            headers={
                "X-API-Key": self.api_key,
            },
        )

        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                if response.status < 200 or response.status >= 300:
                    logging.warning("Backend returned non-2xx status %s", response.status)
                    return None
                return json.load(response)
        except urllib.error.HTTPError as exc:
            logging.warning("Backend status request failed with status %s", exc.code)
            return None
        except urllib.error.URLError as exc:
            logging.warning("Backend status request network error: %s", exc.reason)
            return None
        except Exception as exc:
            logging.warning("Unexpected backend status request error: %s", exc)
            return None

    def _post_json(self, url, payload):
        body = json.dumps(payload).encode("utf-8")
        request = urllib.request.Request(
            url,
            data=body,
            method="POST",
            headers={
                "Content-Type": "application/json",
                "X-API-Key": self.api_key,
            },
        )

        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                if response.status < 200 or response.status >= 300:
                    logging.warning("Backend returned non-2xx status %s", response.status)
                    return False
                return True
        except urllib.error.HTTPError as exc:
            logging.warning("Backend request failed with status %s", exc.code)
            return False
        except urllib.error.URLError as exc:
            logging.warning("Backend request network error: %s", exc.reason)
            return False
        except Exception as exc:
            logging.warning("Unexpected backend request error: %s", exc)
            return False

    def _load_pending(self):
        if not os.path.exists(self.queue_file):
            return []

        try:
            with open(self.queue_file, "r", encoding="utf-8") as queue_handle:
                data = json.load(queue_handle)
                if isinstance(data, list):
                    return data
        except Exception as exc:
            logging.warning("Failed to load pending queue; starting empty: %s", exc)

        return []

    def _persist_pending(self):
        directory = os.path.dirname(self.queue_file)
        if directory:
            os.makedirs(directory, exist_ok=True)

        temp_path = None
        try:
            file_descriptor, temp_path = tempfile.mkstemp(
                prefix=".pending_readings_", suffix=".tmp", dir=directory or "."
            )
            with os.fdopen(file_descriptor, "w", encoding="utf-8") as queue_handle:
                json.dump(self.pending, queue_handle)
                queue_handle.flush()
                os.fsync(queue_handle.fileno())
            os.replace(temp_path, self.queue_file)
        except Exception as exc:
            logging.warning("Failed to persist pending queue: %s", exc)
            if temp_path and os.path.exists(temp_path):
                try:
                    os.remove(temp_path)
                except OSError:
                    pass
