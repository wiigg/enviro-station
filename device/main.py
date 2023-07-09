# main.py
from utilities import get_cpu_temperature, get_serial_number, check_wifi
from hardware_interface import HardwareInterface
from az_transmitter import AzTransmitter
import time
import os
import logging

# Constants
UPDATE_INTERVAL = 145  # milliseconds


def main():
    # Initialise hardware interface and Azure transmitter
    hardware = HardwareInterface()
    transmitter = AzTransmitter(os.getenv["AZURE_CONNECTION_STRING"])

    # Device ID to send to Azure
    id = "raspi-" + get_serial_number()

    # Log Raspberry Pi serial and Wi-Fi status
    logging.info("Raspberry Pi serial: {}".format(get_serial_number()))
    logging.info("Wi-Fi: {}\n".format("connected" if check_wifi() else "disconnected"))

    time_since_update = 0
    update_time = time.time()

    # Main loop to read data, display, and send to Azure
    while True:
        try:
            values = hardware.read_values()
            time_since_update = time.time() - update_time
            if time_since_update > UPDATE_INTERVAL:
                logging.info(values)
                update_time = time.time()
                if transmitter.send_to_azure(values, id):
                    logging.info("Azure response: OK")
                else:
                    logging.warning("Azure response: Failed")
            hardware.display_status()
        except Exception as e:
            logging.warning("Main Loop Exception: {}".format(e))


if __name__ == "__main__":
    main()
