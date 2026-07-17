import { DASHBOARD_DEVICE_ID, LIVE_SOURCE_WINDOW_IDS } from "./dashboardConfig";

const DEFAULT_DOWNSAMPLE_FIELDS = ["pm2", "temperature"];
const PARTICULATE_MAX_FIELDS = [
  ["pm1", "pm1Max"],
  ["pm2", "pm2Max"],
  ["pm10", "pm10Max"]
];
const CHART_AVERAGE_FIELDS = [
  { field: "pm2", outputKey: "pm2Average" },
  { field: "temperature", outputKey: "temperatureAverage" }
];

function formatOpsEventTime(timestamp) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });
}

export function normalizeOpsEvent(rawEvent) {
  if (!rawEvent || typeof rawEvent !== "object") {
    return null;
  }

  const rawTitle = typeof rawEvent.title === "string" ? rawEvent.title.trim() : "";
  const rawDetail = typeof rawEvent.detail === "string" ? rawEvent.detail.trim() : "";
  const timestampRaw =
    typeof rawEvent.timestamp === "number" ? rawEvent.timestamp : Number(rawEvent.timestamp);
  if (!rawTitle || !rawDetail || !Number.isFinite(timestampRaw)) {
    return null;
  }

  const { title, detail } = normalizeOpsEventCopy(rawTitle, rawDetail);

  const idRaw = rawEvent.id;
  const sourceId =
    typeof idRaw === "number" || typeof idRaw === "string" ? String(idRaw) : "event";
  const id = `${sourceId}-${timestampRaw}-${title}-${detail}`.toLowerCase();

  return {
    id,
    time: formatOpsEventTime(timestampRaw),
    title,
    detail
  };
}

function normalizeOpsEventCopy(title, detail) {
  const loweredTitle = title.toLowerCase();
  const loweredDetail = detail.toLowerCase();
  const historyLoadMatch = loweredDetail.match(/readings loaded for (.+) window/);
  const isLegacyHistoryLoad =
    (loweredTitle === "history loaded" || loweredTitle === "history synced") &&
    historyLoadMatch;

  if (isLegacyHistoryLoad) {
    const windowLabel = historyLoadMatch[1];
    const detailCopy =
      windowLabel === "live" || windowLabel === "1h"
        ? "Loaded recent readings from backend live buffer."
        : "Loaded persisted history from db.";

    return {
      title: "History loaded",
      detail: detailCopy
    };
  }

  return { title, detail };
}

export function computeTemperatureDomain(readings) {
  if (!readings.length) {
    return [18, 26];
  }

  let min = readings[0].temperature;
  let max = readings[0].temperature;

  for (const reading of readings) {
    if (reading.temperature < min) {
      min = reading.temperature;
    }
    if (reading.temperature > max) {
      max = reading.temperature;
    }
  }

  const spread = Math.max(max - min, 1);
  const padding = Math.max(0.6, spread * 0.25);
  const lower = Math.floor((min - padding) * 10) / 10;
  const upper = Math.ceil((max + padding) * 10) / 10;
  return [lower, upper];
}

export function downsampleReadings(readings, maxPoints) {
  if (!Array.isArray(readings)) {
    return [];
  }
  if (readings.length <= maxPoints) {
    return readings;
  }
  if (maxPoints <= 1) {
    return readings.slice(0, 1);
  }
  if (maxPoints === 2) {
    return [readings[0], readings[readings.length - 1]];
  }

  return largestTriangleThreeBuckets(readings, maxPoints, DEFAULT_DOWNSAMPLE_FIELDS);
}

export function buildTrendChartData(readings, chartReadings, averageWindowMs) {
  if (!Array.isArray(chartReadings) || chartReadings.length === 0) {
    return [];
  }

  const rollingAverages = rollingAveragesForPoints(
    Array.isArray(readings) ? readings : [],
    chartReadings,
    averageWindowMs,
    CHART_AVERAGE_FIELDS
  );

  return chartReadings.map((reading, index) => ({
    timestamp: reading.timestamp,
    pm2: reading.pm2Max ?? reading.pm2,
    pm2Average: rollingAverages[index]?.pm2Average ?? null,
    temperature: reading.temperature,
    temperatureAverage: rollingAverages[index]?.temperatureAverage ?? null
  }));
}

function rollingAveragesForPoints(readings, points, windowMs, fields) {
  if (!readings.length || !points.length || !Number.isFinite(windowMs) || windowMs <= 0) {
    return points.map(() => ({}));
  }

  const averages = [];
  const totals = Object.fromEntries(fields.map(({ field }) => [field, 0]));
  const counts = Object.fromEntries(fields.map(({ field }) => [field, 0]));
  let startIndex = 0;
  let endIndex = 0;

  for (const point of points) {
    const windowStart = point.timestamp - windowMs;

    while (
      endIndex < readings.length &&
      readings[endIndex].timestamp <= point.timestamp
    ) {
      for (const { field } of fields) {
        const value = readings[endIndex][field];
        if (Number.isFinite(value)) {
          totals[field] += value;
          counts[field] += 1;
        }
      }
      endIndex += 1;
    }

    while (startIndex < endIndex && readings[startIndex].timestamp < windowStart) {
      for (const { field } of fields) {
        const value = readings[startIndex][field];
        if (Number.isFinite(value)) {
          totals[field] -= value;
          counts[field] -= 1;
        }
      }
      startIndex += 1;
    }

    averages.push(
      Object.fromEntries(
        fields.map(({ field, outputKey }) => [
          outputKey,
          counts[field] > 0 ? totals[field] / counts[field] : null
        ])
      )
    );
  }

  return averages;
}

function largestTriangleThreeBuckets(readings, maxPoints, fields) {
  const sampled = [readings[0]];
  const bucketSize = (readings.length - 2) / (maxPoints - 2);
  const fieldScales = metricScales(readings, fields);
  let anchorIndex = 0;

  for (let index = 0; index < maxPoints - 2; index += 1) {
    const averageRangeStart = Math.floor((index + 1) * bucketSize) + 1;
    const averageRangeEnd = Math.min(
      Math.floor((index + 2) * bucketSize) + 1,
      readings.length
    );
    const averagePoint = averageReadingsPoint(
      readings,
      averageRangeStart,
      averageRangeEnd,
      fields
    );

    const rangeStart = Math.min(Math.floor(index * bucketSize) + 1, readings.length - 2);
    const rangeEnd = Math.max(
      rangeStart + 1,
      Math.min(Math.floor((index + 1) * bucketSize) + 1, readings.length - 1)
    );
    const anchor = readings[anchorIndex];

    let selectedIndex = rangeStart;
    let selectedArea = -1;
    for (let candidateIndex = rangeStart; candidateIndex < rangeEnd; candidateIndex += 1) {
      const candidate = readings[candidateIndex];
      const area = largestMetricArea(anchor, candidate, averagePoint, fields, fieldScales);
      if (area > selectedArea) {
        selectedArea = area;
        selectedIndex = candidateIndex;
      }
    }

    sampled.push(readings[selectedIndex]);
    anchorIndex = selectedIndex;
  }

  sampled.push(readings[readings.length - 1]);
  return sampled;
}

function averageReadingsPoint(readings, start, end, fields) {
  if (start >= end) {
    const fallback = readings[Math.min(start, readings.length - 1)];
    return {
      timestamp: fallback.timestamp,
      values: Object.fromEntries(fields.map((field) => [field, readingValue(fallback, field)]))
    };
  }

  let timestampTotal = 0;
  const totals = Object.fromEntries(fields.map((field) => [field, 0]));
  for (let index = start; index < end; index += 1) {
    timestampTotal += readings[index].timestamp;
    for (const field of fields) {
      totals[field] += readingValue(readings[index], field);
    }
  }

  const count = end - start;
  return {
    timestamp: timestampTotal / count,
    values: Object.fromEntries(fields.map((field) => [field, totals[field] / count]))
  };
}

function largestMetricArea(anchor, candidate, averagePoint, fields, fieldScales) {
  let largestArea = 0;
  for (const field of fields) {
    const scale = fieldScales[field] || 1;
    const anchorY = readingValue(anchor, field) / scale;
    const candidateY = readingValue(candidate, field) / scale;
    const averageY = averagePoint.values[field] / scale;
    const area = Math.abs(
      (anchor.timestamp - averagePoint.timestamp) * (candidateY - anchorY) -
        (anchor.timestamp - candidate.timestamp) * (averageY - anchorY)
    );
    if (area > largestArea) {
      largestArea = area;
    }
  }
  return largestArea;
}

function metricScales(readings, fields) {
  const scales = {};
  for (const field of fields) {
    let min = Number.POSITIVE_INFINITY;
    let max = Number.NEGATIVE_INFINITY;
    for (const reading of readings) {
      const value = readingValue(reading, field);
      if (value < min) {
        min = value;
      }
      if (value > max) {
        max = value;
      }
    }
    scales[field] = Math.max(max - min, 1);
  }
  return scales;
}

function readingValue(reading, field) {
  if (field === "pm2" && Number.isFinite(reading.pm2Max)) {
    return reading.pm2Max;
  }
  const value = reading[field];
  return Number.isFinite(value) ? value : 0;
}

export function filterVisibleReadings(readings, rangeMs) {
  if (!Array.isArray(readings) || readings.length === 0) {
    return [];
  }

  const latestTimestamp = readings[readings.length - 1].timestamp;
  const cutoffTimestamp = latestTimestamp - rangeMs;
  return readings.filter((reading) => reading.timestamp >= cutoffTimestamp);
}

function bucketReadingsByTime(readings, bucketMs) {
  const buckets = new Map();

  for (const reading of readings) {
    const bucketId = Math.floor(reading.timestamp / bucketMs);
    const previous = buckets.get(bucketId);
    const next = { ...reading };
    const bucketHasParticulate = previous?.pmAvailable === true || reading.pmAvailable === true;
    next.pmAvailable = bucketHasParticulate;

    for (const [valueField, maxField] of PARTICULATE_MAX_FIELDS) {
      const values = [
        previous?.[maxField],
        previous?.[valueField],
        reading[maxField],
        reading[valueField]
      ].filter(Number.isFinite);
      next[maxField] = values.length ? Math.max(...values) : null;
    }

    buckets.set(bucketId, next);
  }

  return Array.from(buckets.values()).sort(
    (left, right) => left.timestamp - right.timestamp
  );
}

function bucketSizeForWindow(windowOption) {
  const retainedRangeMs = windowOption.retainedRangeMs ?? windowOption.rangeMs;
  return Math.max(1_000, Math.ceil(retainedRangeMs / windowOption.queryMaxPoints));
}

export function buildHistoryUrl(backendBaseUrl, windowOption, nowMs = Date.now()) {
  const url = new URL(`${backendBaseUrl}/api/readings`);
  if (LIVE_SOURCE_WINDOW_IDS.has(windowOption.id)) {
    url.searchParams.set("limit", String(windowOption.queryMaxPoints));
    url.searchParams.set("source", "live");
  } else {
    const fromTimestamp = nowMs - (windowOption.retainedRangeMs ?? windowOption.rangeMs);
    url.searchParams.set("from", String(fromTimestamp));
    url.searchParams.set("to", String(nowMs));
    url.searchParams.set("max_points", String(windowOption.queryMaxPoints));
  }
  if (DASHBOARD_DEVICE_ID) {
    url.searchParams.set("device_id", DASHBOARD_DEVICE_ID);
  }
  return url.toString();
}

export function appendReadingForWindow(readings, reading, windowOption) {
  return mergeReadingsForWindow([readings, [reading]], windowOption);
}

export function mergeReadingsForWindow(readingSets, windowOption) {
  const merged = new Map();
  for (const readings of readingSets) {
    for (const reading of readings) {
      const key = `${reading.deviceId ?? ""}:${reading.timestamp}`;
      merged.set(key, reading);
    }
  }

  const sortedReadings = Array.from(merged.values()).sort(
    (left, right) => left.timestamp - right.timestamp
  );
  if (sortedReadings.length === 0) {
    return [];
  }

  const latestTimestamp = sortedReadings[sortedReadings.length - 1].timestamp;
  const cutoffTimestamp =
    latestTimestamp - (windowOption.retainedRangeMs ?? windowOption.rangeMs);
  const visibleReadings = sortedReadings.filter(
    (reading) => reading.timestamp >= cutoffTimestamp
  );
  const balancedReadings = LIVE_SOURCE_WINDOW_IDS.has(windowOption.id)
    ? visibleReadings
    : bucketReadingsByTime(visibleReadings, bucketSizeForWindow(windowOption));

  return downsampleReadings(balancedReadings, windowOption.queryMaxPoints);
}

export function normalizeInsight(rawInsight) {
  if (!rawInsight || typeof rawInsight !== "object") {
    return null;
  }

  const title = typeof rawInsight.title === "string" ? rawInsight.title.trim() : "";
  const message = typeof rawInsight.message === "string" ? rawInsight.message.trim() : "";
  const topicRaw = typeof rawInsight.topic === "string" ? rawInsight.topic.trim() : "";
  const severityRaw = typeof rawInsight.severity === "string" ? rawInsight.severity.trim() : "";
  const kindRaw = typeof rawInsight.kind === "string" ? rawInsight.kind.trim() : "";
  const topic = normalizeInsightTopic(topicRaw, title, message);
  const severity = severityRaw.toLowerCase();
  const kind = kindRaw.toLowerCase();
  const normalizedSeverity =
    severity === "critical" || severity === "warn" || severity === "info" ? severity : "info";
  const normalizedKind = kind === "alert" || kind === "insight" || kind === "tip" ? kind : "insight";

  if (!title || !message) {
    return null;
  }

  const normalizedTitle = normalizeInsightTextForSeverity(title, normalizedSeverity);
  const normalizedMessage = normalizeInsightTextForSeverity(message, normalizedSeverity);
  const sources = normalizeInsightSources(rawInsight.sources);

  return {
    id: `${normalizedKind}-${normalizedSeverity}-${normalizedTitle}-${normalizedMessage}`.toLowerCase(),
    title: normalizedTitle,
    message: normalizedMessage,
    topic,
    severity: normalizedSeverity,
    kind: normalizedKind,
    sources
  };
}

function normalizeInsightSources(rawSources) {
  if (!Array.isArray(rawSources)) {
    return [];
  }

  const sources = [];
  const seen = new Set();
  for (const rawSource of rawSources) {
    const title = typeof rawSource?.title === "string" ? rawSource.title.trim() : "";
    const rawUrl = typeof rawSource?.url === "string" ? rawSource.url.trim() : "";
    if (!title || !rawUrl) {
      continue;
    }
    try {
      const url = new URL(rawUrl);
      if (url.protocol !== "https:" || seen.has(url.href)) {
        continue;
      }
      seen.add(url.href);
      sources.push({ title, url: url.href });
    } catch {
      continue;
    }
    if (sources.length === 3) {
      break;
    }
  }
  return sources;
}

function normalizeInsightTopic(rawTopic, title, message) {
  const topic = rawTopic.toLowerCase();
  if (
    topic === "air_quality" ||
    topic === "humidity" ||
    topic === "temperature" ||
    topic === "general"
  ) {
    return topic;
  }

  const combined = `${title} ${message}`.toLowerCase();
  if (
    combined.includes("pm2") ||
    combined.includes("pm10") ||
    combined.includes("partic") ||
    combined.includes("air quality")
  ) {
    return "air_quality";
  }
  if (combined.includes("humid")) {
    return "humidity";
  }
  if (combined.includes("temp") || combined.includes("warm") || combined.includes("cool")) {
    return "temperature";
  }
  return "general";
}

export function normalizeInsightTextForSeverity(message, severity) {
  if (severity === "critical") {
    return message;
  }
  return message
    .replaceAll("Critical threshold", "Threshold")
    .replaceAll("critical threshold", "threshold")
    .replaceAll("Critical range", "Noteworthy range")
    .replaceAll("critical range", "noteworthy range")
    .replaceAll("Critically", "Very")
    .replaceAll("critically", "very")
    .replaceAll("Critical", "Watch")
    .replaceAll("critical", "watch")
    .replaceAll("Action recommended", "Watch")
    .replaceAll("action recommended", "watch")
    .replaceAll("Action required", "Watch")
    .replaceAll("action required", "watch")
    .replaceAll("Take action", "Check")
    .replaceAll("take action", "check");
}
