import convertEpochToLocal from "./time";

export const createNewMessage = (message) => ({
  temperature: parseFloat(message.temperature),
  humidity: parseFloat(message.humidity),
  pressure: parseFloat(message.pressure) / 100,
  nh3: parseFloat(message.nh3),
  oxidised: parseFloat(message.oxidised),
  reduced: parseFloat(message.reduced),
  pm1: parseFloat(message.pm1),
  pm25: parseFloat(message.pm2),
  pm10: parseFloat(message.pm10),
  timestamp: convertEpochToLocal(message.timestamp),
});

export const getLatestData = (data, property) =>
  data.length > 0 ? Math.round(data[data.length - 1][property]) : 0;

// calculate percentage change over the last 30 minutes moving average
export const getPercentageChange = (data, property) => {
  const dataPointsInLast30Mins = 60 * 30;

  if (data.length < dataPointsInLast30Mins) {
    return 0;
  }

  const latestData = getLatestData(data, property);
  const sumData = data
    .slice(data.length - dataPointsInLast30Mins, data.length)
    .reduce((a, b) => a + b[property], 0);

  const averageData = sumData / dataPointsInLast30Mins;

  // check if averageData is not 0 to prevent division by zero error
  if (averageData === 0) {
    return 0;
  }

  return Math.round(((latestData - averageData) / averageData) * 100);
};
