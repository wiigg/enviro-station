const { app, output } = require("@azure/functions");

app.eventHub("es-iot-data", {
  connection: "IoTHubConnection",
  eventHubName: "es-iothub",
  cardinality: "many",
  return: output.generic({
    type: "signalR",
    connectionStringSetting: "AzureSignalRConnectionString",
    hubName: "iotdata",
  }),
  handler: (messages, context) => {
    // Ensure messages is always an array
    if (!Array.isArray(messages)) {
      messages = [messages];
    }

    context.log(`Processing ${messages.length} messages`);

    return messages.map((message) => {
      context.log("Event hub message:", message);
      return {
        target: "newMessage",
        arguments: [message],
      };
    });
  },
});
