const NUMERIC_FIELDS = [
  "timestamp",
  "temperature",
  "pressure",
  "humidity",
  "oxidised",
  "reduced",
  "nh3",
  "pm1",
  "pm2",
  "pm10"
];

const DAY_MS = 24 * 60 * 60 * 1000;
const WEEK_MS = 7 * DAY_MS;
const HOUR_MS = 60 * 60 * 1000;
const NEARBY_YESTERDAY_WINDOW_MS = 90 * 60 * 1000;
const NEARBY_LAST_WEEK_WINDOW_MS = 12 * HOUR_MS;
const MIN_BUCKET_SAMPLES = 3;

export function normalizeReading(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }

  const output = {};
  for (const field of NUMERIC_FIELDS) {
    const parsed = Number(raw[field]);
    if (!Number.isFinite(parsed)) {
      return null;
    }
    output[field] = field === "timestamp" ? normalizeTimestamp(parsed) : parsed;
  }

  return output;
}

export function normalizeReadings(rawReadings) {
  if (!Array.isArray(rawReadings)) {
    return [];
  }

  return rawReadings
    .map(normalizeReading)
    .filter(Boolean)
    .sort((a, b) => a.timestamp - b.timestamp);
}

export function appendReading(readings, reading, maxPoints) {
  const next = [...readings, reading];
  if (next.length <= maxPoints) {
    return next;
  }
  return next.slice(next.length - maxPoints);
}

export function buildKpis(readings, windowId = "live") {
  const latest = readings[readings.length - 1];

  if (!latest) {
    return [
      { label: "PM2.5", value: "--", unit: "ug/m3", trend: "Waiting for data", state: "muted" },
      { label: "PM10", value: "--", unit: "ug/m3", trend: "Waiting for data", state: "muted" },
      { label: "Temp", value: "--", unit: "C", trend: "Waiting for data", state: "muted" },
      { label: "Humidity", value: "--", unit: "%", trend: "Waiting for data", state: "muted" }
    ];
  }

  const samples = readings.slice(Math.max(0, readings.length - 30));

  return [
    {
      label: "PM2.5",
      value: latest.pm2.toFixed(1),
      unit: "ug/m3",
      trend: metricTrend(readings, samples, latest, "pm2", "ug/m3", 1, windowId),
      state: severityForPM25(latest.pm2)
    },
    {
      label: "PM10",
      value: latest.pm10.toFixed(1),
      unit: "ug/m3",
      trend: metricTrend(readings, samples, latest, "pm10", "ug/m3", 1, windowId),
      state: severityForPM10(latest.pm10)
    },
    {
      label: "Temp",
      value: latest.temperature.toFixed(1),
      unit: "C",
      trend: metricTrend(readings, samples, latest, "temperature", "C", 1, windowId),
      state: "ok"
    },
    {
      label: "Humidity",
      value: latest.humidity.toFixed(0),
      unit: "%",
      trend: metricTrend(readings, samples, latest, "humidity", "%", 0, windowId),
      state: "ok"
    }
  ];
}

export function getSeries(readings) {
  return {
    particulate: readings.map((item) => item.pm2),
    comfort: readings.map((item) => item.temperature)
  };
}

function average(values) {
  if (!values.length) {
    return 0;
  }
  return values.reduce((total, value) => total + value, 0) / values.length;
}

function metricTrend(readings, samples, latest, field, unit, decimals, windowId) {
  if (windowId === "24h") {
    const yesterdayHourBaseline = averageFieldInWindow(
      readings,
      startOfHour(latest.timestamp - DAY_MS),
      startOfHour(latest.timestamp - DAY_MS) + HOUR_MS,
      field
    );

    if (Number.isFinite(yesterdayHourBaseline)) {
      return describeDelta(latest[field] - yesterdayHourBaseline, unit, decimals, "yesterday");
    }

    const nearbyYesterdayBaseline = averageFieldInWindow(
      readings,
      latest.timestamp - DAY_MS - NEARBY_YESTERDAY_WINDOW_MS,
      latest.timestamp - DAY_MS + NEARBY_YESTERDAY_WINDOW_MS,
      field
    );

    if (Number.isFinite(nearbyYesterdayBaseline)) {
      return describeDelta(latest[field] - nearbyYesterdayBaseline, unit, decimals, "yesterday");
    }
  }

  if (windowId === "7d") {
    const lastWeekHourBaseline = averageFieldInWindow(
      readings,
      startOfHour(latest.timestamp - WEEK_MS),
      startOfHour(latest.timestamp - WEEK_MS) + HOUR_MS,
      field
    );

    if (Number.isFinite(lastWeekHourBaseline)) {
      return describeDelta(
        latest[field] - lastWeekHourBaseline,
        unit,
        decimals,
        "same time last week"
      );
    }

    const nearbyLastWeekBaseline = averageFieldInWindow(
      readings,
      latest.timestamp - WEEK_MS - NEARBY_LAST_WEEK_WINDOW_MS,
      latest.timestamp - WEEK_MS + NEARBY_LAST_WEEK_WINDOW_MS,
      field
    );

    if (Number.isFinite(nearbyLastWeekBaseline)) {
      return describeDelta(
        latest[field] - nearbyLastWeekBaseline,
        unit,
        decimals,
        "same time last week"
      );
    }
  }

  const baseline = average(samples.map((item) => item[field]));
  if (!Number.isFinite(baseline)) {
    return "Waiting for baseline";
  }

  return describeDelta(latest[field] - baseline, unit, decimals, "recent average");
}

function describeDelta(delta, unit, decimals, referenceLabel) {
  const threshold = decimals === 0 ? 1 : 0.1;
  if (Math.abs(delta) < threshold) {
    return `About the same as ${referenceLabel}`;
  }

  const magnitude = Math.abs(delta).toFixed(decimals);
  if (delta > 0) {
    return `${magnitude} ${unit} above ${referenceLabel}`;
  }

  return `${magnitude} ${unit} below ${referenceLabel}`;
}

function severityForPM25(value) {
  if (value <= 5) {
    return "ok";
  }
  if (value <= 15) {
    return "warn";
  }
  return "alert";
}

function severityForPM10(value) {
  if (value <= 15) {
    return "ok";
  }
  if (value <= 45) {
    return "warn";
  }
  return "alert";
}

function normalizeTimestamp(timestamp) {
  // Device/backend currently emit Unix seconds; charting code expects milliseconds.
  if (timestamp < 1_000_000_000_000) {
    return Math.trunc(timestamp * 1000);
  }
  return Math.trunc(timestamp);
}

function startOfHour(timestamp) {
  return timestamp - (timestamp % HOUR_MS);
}

function averageFieldInWindow(readings, startInclusive, endExclusive, field) {
  const values = readings
    .filter((reading) => reading.timestamp >= startInclusive && reading.timestamp < endExclusive)
    .map((reading) => reading[field]);

  if (values.length < MIN_BUCKET_SAMPLES) {
    return Number.NaN;
  }

  return average(values);
}
