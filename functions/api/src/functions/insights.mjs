import { app } from "@azure/functions";
import getCompletion from "../lib/openai.mjs";
import connect, { Sensor } from "../lib/db.mjs";

const formatDataPoint = ({ pm1, pm2, pm10 }) =>
  `PM1: ${pm1} # PM2.5: ${pm2} # PM10: ${pm10} #`;

const getAvgLastHourData = async () => {
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
    {
      $project: {
        _id: 0,
        pm1: { $round: ["$pm1", 1] },
        pm2: { $round: ["$pm2", 1] },
        pm10: { $round: ["$pm10", 1] },
      },
    },
  ]);
};

const getThisTimeYesterdayData = async () => {
  const now = Date.now(); // get current time in milliseconds
  const yesterdaySameTime = now - 24 * 60 * 60 * 1000; // 24 hours ago

  return await Sensor.findOne(
    {
      createdAt: {
        $gt: yesterdaySameTime,
      },
    },
    { pm1: 1, pm2: 1, pm10: 1 }
  ).sort({ createdAt: 1 });
};

const getLatestData = async () => {
  return await Sensor.findOne({}, { pm1: 1, pm2: 1, pm10: 1 }).sort({
    createdAt: -1,
  });
};

app.http("insights", {
  methods: ["GET"],
  authLevel: "anonymous",
  handler: async (request, context) => {
    context.log(`Http function processed request for url "${request.url}"`);

    const style = request.query.get("style") || "Friendly";

    await connect();
    const hourData = await getAvgLastHourData();
    const yesterdayData = await getThisTimeYesterdayData();
    const latestData = await getLatestData();

    if (!hourData || !latestData || !yesterdayData) {
      context.log("Error: No data.");
      return { body: "Error: No data." };
    }

    const hourDataString = formatDataPoint(hourData[0]);
    context.log("Last hour average: ", hourDataString);

    const yesterdayDataString = formatDataPoint(yesterdayData);
    context.log("Yesterday at this time: ", yesterdayDataString);

    const latestDataString = formatDataPoint(latestData);
    context.log("Latest data: ", latestDataString);

    const prompt = `Last hour average data= ${hourDataString} ### Latest data= ${latestDataString} ### Yesterday at this time= ${yesterdayDataString} ###`;
    const aiResponse = await getCompletion(style, prompt);

    return {
      body: aiResponse,
    };
  },
});
