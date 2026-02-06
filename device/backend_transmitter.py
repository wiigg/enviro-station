import json
import logging
import os
import tempfile
import urllib.error
import urllib.request


class BackendTransmitter:
    def __init__(
        self,
        base_url,
        api_key,
        queue_file="pending_readings.json",
        batch_size=100,
        timeout_seconds=5,
        max_pending=5000,
    ):
        if not base_url:
            raise ValueError("BACKEND_BASE_URL is required")
        if not api_key:
            raise ValueError("INGEST_API_KEY is required")

        self.ingest_url = base_url.rstrip("/") + "/api/ingest"
        self.batch_url = base_url.rstrip("/") + "/api/ingest/batch"
        self.api_key = api_key
        self.queue_file = queue_file
        self.batch_size = max(1, batch_size)
        self.timeout_seconds = timeout_seconds
        self.max_pending = max(1, max_pending)
        self.pending = self._load_pending()

    def send(self, reading):
        self.pending.append(reading)
        if len(self.pending) > self.max_pending:
            drop_count = len(self.pending) - self.max_pending
            logging.warning(
                "Pending queue exceeded limit; dropping %s oldest readings", drop_count
            )
            self.pending = self.pending[drop_count:]

        self._persist_pending()
        return self.flush()

    def flush(self):
        if not self.pending:
            return True

        if len(self.pending) == 1:
            if not self._post_json(self.ingest_url, self.pending[0]):
                return False
            self.pending = []
            self._persist_pending()
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

        return len(self.pending) == 0

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
