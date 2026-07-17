import { describe, expect, it } from "vitest";
import { WINDOW_OPTIONS_BY_ID } from "./dashboardConfig";
import {
  buildTrendChartData,
  buildHistoryUrl,
  downsampleReadings,
  mergeReadingsForWindow,
  normalizeInsight,
  normalizeOpsEvent
} from "./dashboardTransforms";

describe("dashboard transforms", () => {
  it("preserves endpoints and a particulate spike while downsampling", () => {
    const readings = Array.from({ length: 100 }, (_item, index) => ({
      timestamp: index * 1_000,
      pm2: index === 50 ? 100 : 5,
      temperature: 20
    }));

    const sampled = downsampleReadings(readings, 12);

    expect(sampled).toHaveLength(12);
    expect(sampled[0]).toBe(readings[0]);
    expect(sampled.at(-1)).toBe(readings.at(-1));
    expect(sampled.some((item) => item.pm2 === 100)).toBe(true);
  });

  it("excludes unavailable particle readings from rolling averages", () => {
    const readings = [
      { timestamp: 1_000, pm2: 10, temperature: 20 },
      { timestamp: 2_000, pm2: null, temperature: 22 },
      { timestamp: 3_000, pm2: 20, temperature: 24 }
    ];
    const chartData = buildTrendChartData(readings, readings, 10_000);

    expect(chartData[1].pm2Average).toBe(10);
    expect(chartData[2].pm2Average).toBe(15);
    expect(chartData[2].temperatureAverage).toBe(22);
  });

  it("builds bounded history requests for persisted windows", () => {
    const now = 1_800_000_000_000;
    const url = new URL(
      buildHistoryUrl("https://api.example.com", WINDOW_OPTIONS_BY_ID["24h"], now)
    );

    expect(url.pathname).toBe("/api/readings");
    expect(url.searchParams.get("to")).toBe(String(now));
    expect(url.searchParams.get("max_points")).toBe("960");
  });

  it("keeps raw live readings from dominating a seven-day window", () => {
    const windowOption = WINDOW_OPTIONS_BY_ID["7d"];
    const endTimestamp = 1_800_000_000_000;
    const bucketMs = Math.ceil(
      windowOption.retainedRangeMs / windowOption.queryMaxPoints
    );
    const startTimestamp = endTimestamp - windowOption.retainedRangeMs;
    const reading = (timestamp, pm2 = 5) => ({
      deviceId: "station-1",
      timestamp,
      temperature: 20,
      pressure: 100_000,
      humidity: 45,
      oxidised: 1,
      reduced: 1,
      nh3: 1,
      pm1: pm2,
      pm2,
      pm10: pm2
    });
    const historicalReadings = Array.from(
      { length: windowOption.queryMaxPoints },
      (_item, index) => reading(startTimestamp + index * bucketMs)
    );
    const liveReadings = Array.from({ length: 1_200 }, (_item, index) =>
      reading(
        endTimestamp - (1_199 - index) * 1_000,
        index === 500 ? 100 : 5
      )
    );

    const merged = mergeReadingsForWindow(
      [historicalReadings, liveReadings],
      windowOption
    );
    const recentReadings = merged.filter(
      (item) => item.timestamp >= endTimestamp - 30 * 60 * 1_000
    );

    expect(merged.length).toBeLessThanOrEqual(windowOption.queryMaxPoints);
    expect(recentReadings.length).toBeLessThanOrEqual(5);
    expect(recentReadings.some((item) => item.pm2Max === 100)).toBe(true);
    expect(merged.at(-1).timestamp).toBe(endTimestamp);
  });

  it("creates stable unique keys when live events reuse a zero database id", () => {
    const first = normalizeOpsEvent({
      id: 0,
      timestamp: 1_800_000_000_000,
      title: "Device connected",
      detail: "Telemetry resumed"
    });
    const second = normalizeOpsEvent({
      id: 0,
      timestamp: 1_800_000_001_000,
      title: "Device disconnected",
      detail: "Telemetry paused"
    });

    expect(first.id).not.toBe(second.id);
  });

  it("keeps only safe HTTPS insight sources", () => {
    const insight = normalizeInsight({
      topic: "temperature",
      kind: "tip",
      severity: "info",
      title: "Cooler outside",
      message: "Brief ventilation may help.",
      sources: [
        { title: "Met Office", url: "https://www.metoffice.gov.uk/weather" },
        { title: "Unsafe", url: "http://example.com/source" }
      ]
    });

    expect(insight.sources).toEqual([
      { title: "Met Office", url: "https://www.metoffice.gov.uk/weather" }
    ]);
  });
});
