import { Schema, model, connect } from "mongoose";

const connectionString = process.env["CosmosDBConnectionString"];

const connectDb = async () =>
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

SensorSchema.set("toJSON", {
  virtuals: true,
  versionKey: false,
  transform: (doc, ret) => {
    ret.id = ret._id;
    delete ret._id;
  },
});

export const Sensor = model("Sensor", SensorSchema);
export default connectDb;
