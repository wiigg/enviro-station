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
      trend: trendText(latest.pm2, average(samples.map((item) => item.pm2))),
      state: severityForPM25(latest.pm2)
    },
    {
      label: "PM10",
      value: latest.pm10.toFixed(1),
      unit: "ug/m3",
      trend: trendText(latest.pm10, average(samples.map((item) => item.pm10))),
      state: severityForPM10(latest.pm10)
    },
    {
      label: "Temp",
      value: latest.temperature.toFixed(1),
      unit: "C",
      trend: trendText(latest.temperature, average(samples.map((item) => item.temperature))),
      state: "ok"
    },
    {
      label: "Humidity",
      value: latest.humidity.toFixed(0),
      unit: "%",
      trend: trendText(latest.humidity, average(samples.map((item) => item.humidity))),
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

function trendText(latest, baseline) {
  if (!baseline) {
    return "No baseline yet";
  }

  const delta = ((latest - baseline) / baseline) * 100;
  const sign = delta >= 0 ? "+" : "";
  return `${sign}${delta.toFixed(1)}% vs 30-sample avg`;
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
