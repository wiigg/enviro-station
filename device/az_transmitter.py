from azure.iot.device import (
    IoTHubSession,
    MQTTConnectionDroppedError,
    MQTTConnectionFailedError,
)
import json
import uuid
import logging


class AzTransmitter:
    def __init__(self, connection_string):
        print("Connecting to IoT Hub...")
        try:
            self.session = IoTHubSession.from_connection_string(connection_string)
            print("Connection established.")
        except MQTTConnectionFailedError:
            logging.warning("Could not connect. Exiting...")
            raise

    async def send_to_azure(self, values, id):
        """Send data to Azure IoT Hub"""
        pm_values = dict(i for i in values.items() if i[0].startswith("P"))
        temp_values = dict(i for i in values.items() if not i[0].startswith("P"))

        pm_values_json = [
            {"value_type": key, "value": val} for key, val in pm_values.items()
        ]
        temp_values_json = [
            {"value_type": key, "value": val} for key, val in temp_values.items()
        ]

        # Combine pm_values_json and temp_values_json
        data = pm_values_json + temp_values_json

        try:
            print("Sending data to IoT Hub...")
            json_data = json.dumps(data)
            await self.session.send_message(json_data)
            print("Message sent.")
            return True
        except MQTTConnectionDroppedError:
            logging.warning("Connection dropped. Exiting...")
            return False
