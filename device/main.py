# main.py
from utilities import get_serial_number, check_wifi
from hardware_interface import HardwareInterface
from az_transmitter import AzTransmitter
from dotenv import load_dotenv
import os
import logging
import asyncio

# Constants
UPDATE_INTERVAL = 0.5  # seconds


async def main():
    load_dotenv()
    connection_string = os.getenv("AZURE_CONNECTION_STRING")
    logging.basicConfig(level=logging.INFO)

    # Log Raspberry Pi serial and Wi-Fi status
    logging.info("Raspberry Pi serial: {}".format(get_serial_number()))
    logging.info("Wi-Fi: {}\n".format("connected" if check_wifi() else "disconnected"))

    # Initialise hardware interface and Azure transmitter
    hardware = HardwareInterface()
    transmitter = AzTransmitter(connection_string)

    # Main loop to read data, display, and send to Azure
    while True:
        try:
            values = hardware.read_values()
            logging.info(values)
            await transmitter.send_to_azure(values)
            hardware.display_status()
            await asyncio.sleep(UPDATE_INTERVAL)
        except Exception as e:
            logging.warning("Main exception: {}".format(e))
            break


if __name__ == "__main__":
    asyncio.run(main())
