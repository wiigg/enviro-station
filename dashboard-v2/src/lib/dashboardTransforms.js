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
  const id =
    typeof idRaw === "number" || typeof idRaw === "string"
      ? String(idRaw)
      : `${timestampRaw}-${title}-${detail}`.toLowerCase();

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
  if (!Array.isArray(readings) || readings.length <= maxPoints) {
    return readings;
  }

  const stride = Math.ceil(readings.length / maxPoints);
  return readings.filter((_, index) => index % stride === 0);
}

export function filterVisibleReadings(readings, rangeMs) {
  if (!Array.isArray(readings) || readings.length === 0) {
    return [];
  }

  const latestTimestamp = readings[readings.length - 1].timestamp;
  const cutoffTimestamp = latestTimestamp - rangeMs;
  return readings.filter((reading) => reading.timestamp >= cutoffTimestamp);
}

export function buildHistoryUrl(backendBaseUrl, windowOption, nowMs = Date.now()) {
  if (LIVE_SOURCE_WINDOW_IDS.has(windowOption.id)) {
    return `${backendBaseUrl}/api/readings?limit=${windowOption.queryMaxPoints}&source=live`;
  }

  const fromTimestamp = nowMs - (windowOption.retainedRangeMs ?? windowOption.rangeMs);
  return `${backendBaseUrl}/api/readings?from=${fromTimestamp}&to=${nowMs}&max_points=${windowOption.queryMaxPoints}`;
}

export function appendReadingForWindow(readings, reading, windowOption) {
  const cutoffTimestamp =
    reading.timestamp - (windowOption.retainedRangeMs ?? windowOption.rangeMs);
  const nextReadings = [...readings, reading].filter(
    (existingReading) => existingReading.timestamp >= cutoffTimestamp
  );
  return downsampleReadings(nextReadings, windowOption.queryMaxPoints);
}

export function normalizeInsight(rawInsight) {
  if (!rawInsight || typeof rawInsight !== "object") {
    return null;
  }

  const title = typeof rawInsight.title === "string" ? rawInsight.title.trim() : "";
  const message = typeof rawInsight.message === "string" ? rawInsight.message.trim() : "";
  const severityRaw = typeof rawInsight.severity === "string" ? rawInsight.severity.trim() : "";
  const kindRaw = typeof rawInsight.kind === "string" ? rawInsight.kind.trim() : "";
  const severity = severityRaw.toLowerCase();
  const kind = kindRaw.toLowerCase();
  const normalizedSeverity =
    severity === "critical" || severity === "warn" || severity === "info" ? severity : "info";
  const normalizedKind = kind === "alert" || kind === "insight" || kind === "tip" ? kind : "insight";

  if (!title || !message) {
    return null;
  }

  return {
    id: `${normalizedKind}-${normalizedSeverity}-${title}-${message}`.toLowerCase(),
    title,
    message,
    severity: normalizedSeverity,
    kind: normalizedKind
  };
}
import { LIVE_SOURCE_WINDOW_IDS } from "./dashboardConfig";
