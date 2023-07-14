import { app, output } from "@azure/functions";
import connection, { Sensor } from "../lib/db.mjs";

app.eventHub("es-iot-data", {
  connection: "IoTHubConnection",
  eventHubName: "es-iothub",
  cardinality: "many",
  return: output.generic({
    type: "signalR",
    connectionStringSetting: "AzureSignalRConnectionString",
    hubName: "iotdata",
  }),
  handler: async (messages, context) => {
    // Ensure messages is always an array
    if (!Array.isArray(messages)) {
      messages = [messages];
    }

    await connection();

    // context.log(`Processing ${messages.length} messages`);

    const data = messages.map((message) => {
      context.log("Event hub message:", message);

      const datapoint = new Sensor({
        temperature: message.temperature,
        pressure: message.pressure,
        humidity: message.humidity,
        oxidised: message.oxidised,
        reduced: message.reduced,
        nh3: message.nh3,
        p1: message.P1,
        p2: message.P2,
        p10: message.P10,
      });

      datapoint.save();  // fire and forget

      return {
        target: "newMessage",
        arguments: [message],
      };
    });

    return data;
  },
});
