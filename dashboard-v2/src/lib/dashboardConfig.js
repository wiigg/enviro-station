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
    queryMaxPoints: 900,
    chartPoints: 900,
    cacheTtlMs: 15000
  },
  {
    id: "1h",
    label: "1h",
    rangeMs: HOUR_MS,
    retainedRangeMs: HOUR_MS,
    queryMaxPoints: 3600,
    chartPoints: 1800,
    cacheTtlMs: 30000
  },
  {
    id: "24h",
    label: "24h",
    rangeMs: DAY_MS,
    retainedRangeMs: DAY_MS + YESTERDAY_LOOKBACK_MS,
    queryMaxPoints: 7200,
    chartPoints: 7200,
    cacheTtlMs: 120000
  },
  {
    id: "7d",
    label: "7d",
    rangeMs: WEEK_MS,
    retainedRangeMs: WEEK_MS + LAST_WEEK_LOOKBACK_MS,
    queryMaxPoints: 7000,
    chartPoints: 7000,
    cacheTtlMs: 300000
  }
];

export const WINDOW_OPTIONS_BY_ID = Object.fromEntries(
  WINDOW_OPTIONS.map((windowOption) => [windowOption.id, windowOption])
);

export const PREFETCH_WINDOW_IDS = ["1h", "24h"];
export const STREAM_WINDOW_IDS = ["live", "1h"];
export const LIVE_SOURCE_WINDOW_IDS = new Set(["live", "1h"]);

export const INSIGHT_POLL_INTERVAL_MS = 30 * MINUTE_MS;
export const INSIGHT_MAX_ITEMS = 3;
export const OPS_FEED_POLL_INTERVAL_MS = 15000;
export const OPS_FEED_MAX_ITEMS = 6;
