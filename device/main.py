# main.py
import os
import logging
import asyncio

from dotenv import load_dotenv

from az_transmitter import AzTransmitter
from device_utilities import get_serial_number, check_wifi
from device_interface import DeviceInterface

# Constants
UPDATE_INTERVAL = 1  # seconds


async def main():
    load_dotenv()
    connection_string = os.getenv("AZURE_CONNECTION_STRING")
    logging.basicConfig(level=logging.INFO)

    # Log Raspberry Pi serial and Wi-Fi status
    serial_number = get_serial_number()
    wifi_status = "connected" if check_wifi() else "disconnected"
    logging.info(f"Raspberry Pi serial: {serial_number}")
    logging.info(f"Wi-Fi: {wifi_status}\n")

    # Initialise device interface and Azure transmitter
    device = DeviceInterface()
    transmitter = AzTransmitter(connection_string)

    # Main loop to read data, display, and send to Azure
    while True:
        try:
            values = device.read_values()
            logging.info(values)
            await transmitter.send_to_azure(values)
            device.display_status()
            await asyncio.sleep(UPDATE_INTERVAL)
        except Exception as e:
            logging.warning(f"Main exception: {e}")
            break


if __name__ == "__main__":
    asyncio.run(main())
