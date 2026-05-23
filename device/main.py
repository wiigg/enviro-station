# main.py
import os
import logging
import time

from dotenv import load_dotenv

from backend_transmitter import BackendTransmitter
from device_utilities import get_serial_number, check_wifi
from device_interface import DeviceInterface

UPDATE_INTERVAL = 1  # seconds


def env_int(name, fallback):
    raw_value = os.getenv(name)
    if raw_value is None:
        return fallback
    try:
        parsed = int(raw_value)
    except ValueError:
        return fallback
    return parsed


def env_bool(name, fallback):
    raw_value = os.getenv(name)
    if raw_value is None:
        return fallback
    normalized = raw_value.strip().lower()
    if normalized in ("1", "true", "yes", "y", "on"):
        return True
    if normalized in ("0", "false", "no", "n", "off"):
        return False
    return fallback


def main():
    load_dotenv(".env")
    backend_base_url = os.getenv("BACKEND_BASE_URL")
    ingest_api_key = os.getenv("INGEST_API_KEY")
    queue_file = os.getenv("DEVICE_QUEUE_FILE", "pending_readings.json")
    batch_size = env_int("DEVICE_BATCH_SIZE", 1000)
    timeout_seconds = env_int("DEVICE_HTTP_TIMEOUT_SECONDS", 5)
    max_pending = env_int("DEVICE_MAX_PENDING", 5000)
    flush_interval_seconds = env_int("DEVICE_FLUSH_INTERVAL_SECONDS", 60)
    read_interval_seconds = env_int("DEVICE_READ_INTERVAL_SECONDS", UPDATE_INTERVAL)
    live_interval_seconds = env_int("DEVICE_LIVE_INTERVAL_SECONDS", 1)
    live_status_interval_seconds = env_int("DEVICE_LIVE_STATUS_INTERVAL_SECONDS", 10)
    live_status_idle_max_seconds = env_int("DEVICE_LIVE_STATUS_IDLE_MAX_SECONDS", 900)
    live_require_subscriber = env_bool("DEVICE_LIVE_REQUIRE_SUBSCRIBER", True)
    logging.basicConfig(level=logging.INFO)

    # Log Raspberry Pi serial and Wi-Fi status
    serial_number = get_serial_number()
    wifi_status = "connected" if check_wifi() else "disconnected"
    logging.info(f"Raspberry Pi serial: {serial_number}")
    logging.info(f"Wi-Fi: {wifi_status}\n")

    # Initialise device interface and backend transmitter
    device = DeviceInterface()
    transmitter = BackendTransmitter(
        base_url=backend_base_url,
        api_key=ingest_api_key,
        queue_file=queue_file,
        batch_size=batch_size,
        timeout_seconds=timeout_seconds,
        max_pending=max_pending,
        flush_interval_seconds=flush_interval_seconds,
        live_interval_seconds=live_interval_seconds,
        live_require_subscriber=live_require_subscriber,
        live_status_interval_seconds=live_status_interval_seconds,
        live_status_idle_max_seconds=live_status_idle_max_seconds,
        device_id=serial_number,
    )
    transmitter.flush()

    # Main loop to read data, display, and send to backend
    while True:
        try:
            values = device.read_values()
            logging.info(values)
            transmitter.send(values)
            device.display_status(values)
            time.sleep(max(1, read_interval_seconds))
        except KeyboardInterrupt:
            transmitter.flush()
            logging.info("Shutting down device loop.")
            break
        except Exception as e:
            logging.warning(f"Main exception: {e}")
            time.sleep(max(1, read_interval_seconds))


if __name__ == "__main__":
    main()
