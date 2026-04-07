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

const MINUTE_MS = 60 * 1000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;
const WEEK_MS = 7 * DAY_MS;
const HOUR_AGO_BASELINE_WINDOW_MS = 10 * MINUTE_MS;
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

  const visibleReadings = readingsForWindow(readings, latest.timestamp, windowId);
  const samples = visibleReadings.slice(Math.max(0, visibleReadings.length - 30), -1);

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

function average(values) {
  if (!values.length) {
    return Number.NaN;
  }
  return values.reduce((total, value) => total + value, 0) / values.length;
}

function metricTrend(readings, samples, latest, field, unit, decimals, windowId) {
  if (windowId === "live") {
    const recentBaseline = average(samples.map((item) => item[field]));
    if (Number.isFinite(recentBaseline)) {
      return describeDelta(latest[field] - recentBaseline, unit, decimals, "recently");
    }
  }

  if (windowId === "1h") {
    const exactHourAgoBaseline = averageFieldInWindow(
      readings,
      latest.timestamp - HOUR_MS,
      latest.timestamp - HOUR_MS + HOUR_AGO_BASELINE_WINDOW_MS,
      field
    );
    const hourAgoBaseline = Number.isFinite(exactHourAgoBaseline)
      ? exactHourAgoBaseline
      : Number.NaN;

    if (Number.isFinite(hourAgoBaseline)) {
      return describeDelta(latest[field] - hourAgoBaseline, unit, decimals, "an hour ago");
    }
  }

  if (windowId === "24h") {
    const yesterdayHourBaseline = averageFieldInWindow(
      readings,
      startOfHour(latest.timestamp - DAY_MS),
      startOfHour(latest.timestamp - DAY_MS) + HOUR_MS,
      field
    );

    if (Number.isFinite(yesterdayHourBaseline)) {
      return describeDelta(
        latest[field] - yesterdayHourBaseline,
        unit,
        decimals,
        "same time yesterday"
      );
    }

    const nearbyYesterdayBaseline = averageFieldInWindow(
      readings,
      latest.timestamp - DAY_MS - NEARBY_YESTERDAY_WINDOW_MS,
      latest.timestamp - DAY_MS + NEARBY_YESTERDAY_WINDOW_MS,
      field
    );

    if (Number.isFinite(nearbyYesterdayBaseline)) {
      return describeDelta(
        latest[field] - nearbyYesterdayBaseline,
        unit,
        decimals,
        "same time yesterday"
      );
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

  const oldestVisibleReading = oldestReadingForWindow(readings, latest.timestamp, windowId);
  if (oldestVisibleReading && oldestVisibleReading.timestamp < latest.timestamp) {
    const fallbackReferenceLabel =
      windowId === "1h"
        ? "last hour"
        : referenceLabelForElapsed(latest.timestamp - oldestVisibleReading.timestamp);

    return describeDelta(
      latest[field] - oldestVisibleReading[field],
      unit,
      decimals,
      fallbackReferenceLabel
    );
  }

  const baseline = average(samples.map((item) => item[field]));
  if (!Number.isFinite(baseline)) {
    return "Waiting for baseline";
  }

  return describeDelta(latest[field] - baseline, unit, decimals, "recently");
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

function readingsForWindow(readings, latestTimestamp, windowId) {
  const rangeMs = rangeForWindow(windowId);
  if (!Number.isFinite(rangeMs)) {
    return readings;
  }

  const cutoffTimestamp = latestTimestamp - rangeMs;
  return readings.filter((reading) => reading.timestamp >= cutoffTimestamp);
}

function oldestReadingForWindow(readings, latestTimestamp, windowId) {
  const visibleReadings = readingsForWindow(readings, latestTimestamp, windowId);
  return visibleReadings[0] ?? null;
}

function rangeForWindow(windowId) {
  if (windowId === "live") {
    return 15 * MINUTE_MS;
  }
  if (windowId === "1h") {
    return HOUR_MS;
  }
  if (windowId === "24h") {
    return DAY_MS;
  }
  if (windowId === "7d") {
    return WEEK_MS;
  }
  return Number.NaN;
}

function referenceLabelForElapsed(elapsedMs) {
  if (elapsedMs >= DAY_MS) {
    return formatElapsedLabel(Math.round(elapsedMs / DAY_MS), "day");
  }
  if (elapsedMs >= HOUR_MS) {
    return formatElapsedLabel(Math.round(elapsedMs / HOUR_MS), "hour");
  }
  if (elapsedMs >= MINUTE_MS) {
    return formatElapsedLabel(Math.round(elapsedMs / MINUTE_MS), "minute");
  }
  return "moments ago";
}

function formatElapsedLabel(value, unit) {
  if (value === 1) {
    return `1 ${unit} ago`;
  }
  return `${value} ${unit}s ago`;
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
