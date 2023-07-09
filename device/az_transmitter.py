from azure.iot.device import IoTHubDeviceClient, Message
import json
import uuid
import logging


class AzTransmitter:
    def __init__(self, connection_string):
        self.client = IoTHubDeviceClient.create_from_connection_string(
            connection_string
        )

        self.client.connect()

    def send_to_azure(self, values, id):
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
            print("Sending data to Azure IoT Hub...")
            json_data = json.dumps(data)
            message = Message(json_data)
            message.message_id = uuid.uuid4()
            message.content_encoding = "utf-8"
            message.content_type = "application/json"
            self.client.send_message(message)
            return True
        except Exception as e:
            logging.warning(f"Failed to send data to Azure IoT Hub: {e}")
            self.disconnect()
            return False

    def disconnect(self):
        self.device_client.shutdown()
