import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from device.backend_transmitter import BackendTransmitter


class BackendTransmitterTest(unittest.TestCase):
    def test_live_and_durable_readings_use_independent_cadences(self):
        with tempfile.TemporaryDirectory() as temp_directory:
            queue_file = str(Path(temp_directory) / "pending.json")
            with patch("device.backend_transmitter.time.monotonic", return_value=0):
                transmitter = BackendTransmitter(
                    base_url="https://example.test",
                    api_key="test-key",
                    queue_file=queue_file,
                    durable_interval_seconds=60,
                    flush_interval_seconds=1800,
                    live_interval_seconds=30,
                    live_require_subscriber=False,
                )

            posted = []
            transmitter._post_json = lambda url, payload: posted.append(
                (url, payload)
            ) or True

            readings = [
                {"timestamp": 1, "temperature": 20.0},
                {"timestamp": 2, "temperature": 20.1},
                {"timestamp": 3, "temperature": 20.2},
                {"timestamp": 4, "temperature": 20.3},
            ]
            for now, reading in zip((0, 10, 30, 60), readings):
                with patch(
                    "device.backend_transmitter.time.monotonic", return_value=now
                ):
                    self.assertTrue(transmitter.send(reading))

            self.assertEqual(
                [reading["timestamp"] for reading in transmitter.pending], [1, 4]
            )
            self.assertEqual(
                [payload["timestamp"] for _, payload in posted], [1, 3, 4]
            )


if __name__ == "__main__":
    unittest.main()
