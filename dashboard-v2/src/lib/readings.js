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
const YESTERDAY_TOLERANCE_MS = 2 * 60 * 60 * 1000;

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
    output[field] = parsed;
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

export function buildKpis(readings) {
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
      trend: metricTrend(readings, samples, latest, "pm2", "ug/m3", 1),
      state: severityForPM25(latest.pm2)
    },
    {
      label: "PM10",
      value: latest.pm10.toFixed(1),
      unit: "ug/m3",
      trend: metricTrend(readings, samples, latest, "pm10", "ug/m3", 1),
      state: severityForPM10(latest.pm10)
    },
    {
      label: "Temp",
      value: latest.temperature.toFixed(1),
      unit: "C",
      trend: metricTrend(readings, samples, latest, "temperature", "C", 1),
      state: "ok"
    },
    {
      label: "Humidity",
      value: latest.humidity.toFixed(0),
      unit: "%",
      trend: metricTrend(readings, samples, latest, "humidity", "%", 0),
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

function metricTrend(readings, samples, latest, field, unit, decimals) {
  const yesterdayReading = findClosestReading(
    readings,
    latest.timestamp - DAY_MS,
    YESTERDAY_TOLERANCE_MS
  );

  if (yesterdayReading) {
    return describeDelta(
      latest[field] - yesterdayReading[field],
      unit,
      decimals,
      "this time yesterday"
    );
  }

  const baseline = average(samples.map((item) => item[field]));
  if (!Number.isFinite(baseline)) {
    return "Waiting for baseline";
  }

  return describeDelta(latest[field] - baseline, unit, decimals, "recent average");
}

function findClosestReading(readings, targetTimestamp, maxDistanceMs) {
  if (!readings.length) {
    return null;
  }

  let closest = null;
  let closestDistance = Number.POSITIVE_INFINITY;

  for (const reading of readings) {
    const distance = Math.abs(reading.timestamp - targetTimestamp);
    if (distance < closestDistance) {
      closest = reading;
      closestDistance = distance;
    }
  }

  if (closestDistance > maxDistanceMs) {
    return null;
  }

  return closest;
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
