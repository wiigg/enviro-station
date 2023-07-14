import json
import logging

from azure.iot.device import IoTHubSession, MQTTConnectionFailedError, MQTTError


class AzTransmitter:
    def __init__(self, connection_string):
        self.connection_string = connection_string

    async def send_to_azure(self, values):
        """Send data to Azure IoT Hub"""
        json_data = json.dumps(values)

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
