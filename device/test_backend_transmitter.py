import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from device.backend_transmitter import BackendTransmitter


def batch_response(payload):
    response = io.BytesIO(json.dumps(payload).encode("utf-8"))
    response.status = 202
    return response


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

    def test_only_confirmed_chunks_are_removed_across_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            queue_file = Path(directory) / "pending.json"
            readings = [{"timestamp": 1}, {"timestamp": 2}]
            queue_file.write_text(json.dumps(readings), encoding="utf-8")
            options = {"queue_file": queue_file, "batch_size": 1}
            transmitter = BackendTransmitter("https://example.test", "test-key", **options)
            responses = [
                batch_response({"persisted": True}),
                batch_response({"persisted": False}),
            ]
            with patch("urllib.request.urlopen", side_effect=responses):
                self.assertFalse(transmitter.flush())
            self.assertEqual(json.loads(queue_file.read_text()), readings[1:])

            restarted = BackendTransmitter("https://example.test", "test-key", **options)
            self.assertEqual(restarted.pending, readings[1:])
            response = batch_response({"persisted": True})
            with patch("urllib.request.urlopen", return_value=response) as post:
                self.assertTrue(restarted.flush())
            self.assertEqual(json.loads(post.call_args.args[0].data), readings[1:])
            self.assertEqual(json.loads(queue_file.read_text()), [])

    def test_ambiguous_success_response_leaves_queue_intact(self):
        with tempfile.TemporaryDirectory() as directory:
            transmitter = BackendTransmitter(
                "https://example.test",
                "test-key",
                queue_file=Path(directory) / "queue.json",
                live_interval_seconds=0,
            )
            transmitter.send({"timestamp": 1})
            for acknowledgement in (
                {"status": "accepted", "ingested": 1}, {"persisted": "true"}, None
            ):
                with self.subTest(acknowledgement=acknowledgement):
                    response = batch_response(acknowledgement)
                    with patch("urllib.request.urlopen", return_value=response):
                        self.assertFalse(transmitter.flush())
                    self.assertEqual(len(transmitter.pending), 1)

    def test_failed_flush_backs_off_and_success_restores_normal_cadence(self):
        with (
            tempfile.TemporaryDirectory() as directory,
            patch("device.backend_transmitter.time.monotonic") as clock,
        ):
            clock.return_value = 0
            transmitter = BackendTransmitter(
                "https://example.test",
                "test-key",
                queue_file=Path(directory) / "queue.json",
                flush_interval_seconds=60,
                live_interval_seconds=0,
            )
            attempted_at = []

            def respond(request, timeout):
                attempted_at.append(clock.return_value)
                return batch_response({"persisted": clock.return_value >= 75})

            with patch("urllib.request.urlopen", side_effect=respond):
                for now in range(136):
                    clock.return_value = now
                    transmitter.send({"timestamp": now})
            self.assertEqual(attempted_at, [60, 65, 75, 135])


if __name__ == "__main__":
    unittest.main()
