import { Schema, model, connect } from "mongoose";

const connectionString = process.env["CosmosDBConnectionString"];
console.log("MongoDB connection string: ", connectionString);

const connection = async () =>
  connect(connectionString, {
    useNewUrlParser: true,
    useUnifiedTopology: true,
  });

const SensorSchema = new Schema(
  {
    temperature: Number,
    pressure: Number,
    humidity: Number,
    oxidised: Number,
    reduced: Number,
    nh3: Number,
    pm1: Number, 
    pm2: Number,
    pm10: Number,
  },
  { timestamps: true }
);

export const Sensor = model("Sensor", SensorSchema);

export default connection;
