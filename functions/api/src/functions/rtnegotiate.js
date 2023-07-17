const { app, input } = require("@azure/functions");

app.http("negotiate", {
  methods: ["POST"],
  authLevel: "host",
  extraInputs: [
    input.generic({
      type: "signalRConnectionInfo",
      connectionStringSetting: "AzureSignalRConnectionString",
      hubName: "iotdata",
      name: "connectionInfo",
    }),
  ],
  handler: async (request, context) => {
    const connection = JSON.stringify(context.extraInputs.get("connectionInfo"));
    context.log("Connection info:", connection);
    return { body: connection }
  },
});
