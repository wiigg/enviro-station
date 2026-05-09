const MINUTE_MS = 60 * 1000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;
const WEEK_MS = 7 * DAY_MS;
const YESTERDAY_LOOKBACK_MS = 90 * MINUTE_MS;
const LAST_WEEK_LOOKBACK_MS = 12 * HOUR_MS;

export const WINDOW_OPTIONS = [
  {
    id: "live",
    label: "Live",
    rangeMs: 15 * MINUTE_MS,
    retainedRangeMs: 15 * MINUTE_MS,
    queryMaxPoints: 720,
    chartPoints: 600,
    trendAverageWindowMs: MINUTE_MS,
    cacheTtlMs: 15000
  },
  {
    id: "1h",
    label: "1h",
    rangeMs: HOUR_MS,
    retainedRangeMs: HOUR_MS,
    queryMaxPoints: 900,
    chartPoints: 600,
    trendAverageWindowMs: 5 * MINUTE_MS,
    cacheTtlMs: 30000
  },
  {
    id: "24h",
    label: "24h",
    rangeMs: DAY_MS,
    retainedRangeMs: DAY_MS + YESTERDAY_LOOKBACK_MS,
    queryMaxPoints: 960,
    chartPoints: 720,
    trendAverageWindowMs: HOUR_MS,
    cacheTtlMs: 120000
  },
  {
    id: "7d",
    label: "7d",
    rangeMs: WEEK_MS,
    retainedRangeMs: WEEK_MS + LAST_WEEK_LOOKBACK_MS,
    queryMaxPoints: 1200,
    chartPoints: 720,
    trendAverageWindowMs: 6 * HOUR_MS,
    cacheTtlMs: 300000
  }
];

export const WINDOW_OPTIONS_BY_ID = Object.fromEntries(
  WINDOW_OPTIONS.map((windowOption) => [windowOption.id, windowOption])
);

export const PREFETCH_WINDOW_IDS = ["1h"];
export const STREAM_WINDOW_IDS = ["live", "1h", "24h", "7d"];
export const LIVE_SOURCE_WINDOW_IDS = new Set(["live"]);

export const INSIGHT_POLL_INTERVAL_MS = 5 * MINUTE_MS;
export const INSIGHT_MAX_ITEMS = 3;
export const OPS_FEED_POLL_INTERVAL_MS = 5 * MINUTE_MS;
export const OPS_FEED_MAX_ITEMS = 6;

export const DASHBOARD_DEVICE_ID =
  typeof import.meta.env.VITE_DEVICE_ID === "string"
    ? import.meta.env.VITE_DEVICE_ID.trim()
    : "";
