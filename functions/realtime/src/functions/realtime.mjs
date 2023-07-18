import { app, input } from "@azure/functions";

app.http("negotiate", {
  methods: ["GET", "POST"],
  authLevel: "anonymous",
  extraInputs: [
    input.generic({
      type: "signalRConnectionInfo",
      connectionStringSetting: "AzureSignalRConnectionString",
      hubName: "iotdata",
      name: "connectionInfo",
    }),
  ],
  handler: async (request, context) => {
    context.log("Negotiate request received");
    const connection = JSON.stringify(context.extraInputs.get("connectionInfo"));
    context.log("Connection info:", connection);
    return { body: connection }
  },
});
