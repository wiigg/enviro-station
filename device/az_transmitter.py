from azure.iot.device import IoTHubSession, MQTTConnectionFailedError, MQTTError
import json
import logging
import asyncio


class AzTransmitter:
    def __init__(self, connection_string):
        self.connection_string = connection_string

    async def send_to_azure(self, values):
        """Send data to Azure IoT Hub"""
        print(values)
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
        json_data = json.dumps(data)

        try:
            logging.info("Sending data to IoT Hub...")
            async with IoTHubSession.from_connection_string(
                self.connection_string
            ) as session:
                logging.info("Session created.")
                await session.send_message(json_data)
                logging.info("Message sent.")
        except MQTTConnectionFailedError:
            logging.error("Connection to IoT Hub failed.")
        except MQTTError:
            logging.error("An MQTT error occurred.")
        except Exception as e:
            logging.error(f"An unexpected exception occurred: {e}")
        finally:
            logging.info("Connection closed.")
