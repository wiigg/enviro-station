# main.py
import os
import logging
import time

from dotenv import load_dotenv

from backend_transmitter import BackendTransmitter
from device_utilities import get_serial_number, check_wifi
from device_interface import DeviceInterface

UPDATE_INTERVAL = 1  # seconds


def main():
    load_dotenv()
    backend_base_url = os.getenv("BACKEND_BASE_URL")
    ingest_api_key = os.getenv("INGEST_API_KEY")
    queue_file = os.getenv("DEVICE_QUEUE_FILE", "pending_readings.json")
    batch_size = int(os.getenv("DEVICE_BATCH_SIZE", "100"))
    timeout_seconds = int(os.getenv("DEVICE_HTTP_TIMEOUT_SECONDS", "5"))
    max_pending = int(os.getenv("DEVICE_MAX_PENDING", "5000"))
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
    )
    transmitter.flush()

    # Main loop to read data, display, and send to backend
    while True:
        try:
            values = device.read_values()
            logging.info(values)
            transmitter.send(values)
            device.display_status()
            time.sleep(UPDATE_INTERVAL)
        except Exception as e:
            logging.warning(f"Main exception: {e}")
            break


if __name__ == "__main__":
    main()
