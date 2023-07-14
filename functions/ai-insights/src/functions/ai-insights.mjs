import { app } from "@azure/functions";
import getCompletion from "../lib/openai.mjs";
import connect, { Sensor } from "../lib/db.mjs";

const formatDataPoint = ({ pm1, pm2, pm10 }) =>
  `PM_1: ${pm1}, PM_2.5: ${pm2}, PM_10: ${pm10}`;

const getData = async () => {
  await connect();
  return await Sensor.aggregate([
    { $sort: { createdAt: -1 } },
    { $limit: 3600 },
    {
      $group: {
        _id: null,
        pm1: { $avg: "$pm1" },
        pm2: { $avg: "$pm2" },
        pm10: { $avg: "$pm10" },
      },
    },
  ]);
};

app.http("ai-insights", {
  methods: ["GET", "POST"],
  authLevel: "anonymous",
  handler: async (request, context) => {
    context.log(`Http function processed request for url "${request.url}"`);

    const style = request.query.style || "Friendly";

    const data = await getData();

    if (!data.length) {
      context.log("No data.");
      return { body: "No data." };
    }

    const dataString = formatDataPoint(data[0]);
    context.log(dataString);

    const aiResponse = await getCompletion(style, dataString);

    return {
      body: aiResponse,
    };
  },
});
