const mongoose = require("mongoose");

let db = null;

const dataSchema = new mongoose.Schema(
  {
    _id: Number,
    temperature: Number,
    pressure: Number,
    humidity: Number,
    P25: Number,
    P10: Number,
    P1: Number,
  },
  { timestamps: true }
);

module.exports = async function (context, IoTHubMessages) {
  try {
    if (!db) {
      db = await mongoose.connect(process.env["CosmosDbConnection"]);
      context.log("Connection with Cosmos DB established");
    }

    const SensorData = db.model('sensordata', dataSchema);

    context.log(
      `JavaScript eventhub trigger function called for message array: ${IoTHubMessages}`
    );

    IoTHubMessages.forEach((message) => {
      context.log(`Processed message: ${message}`);
      const convData = JSON.parse(message);

      new SensorData({
        _id: convData.curtime,
        temperature: convData.temperature,
        pressure: convData.pressure,
        humidity: convData.humidity,
        P25: convData.P25,
        P10: convData.P10,
        P1: convData.P1,
      }).save();

      context.log(`Message saved to Mongo DB`);
    });

    context.done();
  } catch (err) {
    context.log(`*** Error: ${err}`);
  }
};
