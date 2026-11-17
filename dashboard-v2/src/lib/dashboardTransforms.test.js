import { describe, expect, it } from "vitest";
import { WINDOW_OPTIONS_BY_ID } from "./dashboardConfig";
import { buildKpis, normalizeReading } from "./readings";
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

  it.each(["1h", "24h", "7d"])("preserves peaks and reports sensor dropout in %s", (id) => {
    const windowOption = WINDOW_OPTIONS_BY_ID[id];
    const bucketMs = Math.ceil(windowOption.retainedRangeMs / windowOption.queryMaxPoints);
    const timestamp = Math.floor(1_800_000_000_000 / bucketMs) * bucketMs;
    const good = {
      timestamp, temperature: 20, humidity: 40,
      pmAvailable: true, pm1: 3, pm2: 5, pm10: 9
    };
    const missing = {
      ...good, timestamp: timestamp + 1000,
      pmAvailable: false, pm1: null, pm2: null, pm10: null
    };
    const merged = mergeReadingsForWindow([[good], [missing]], windowOption);

    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({ pmAvailable: false, pm2Max: 5, pm10Max: 9 });
    expect(buildKpis(merged, id).slice(0, 2)).toEqual([
      expect.objectContaining({ value: "--", state: "muted", trend: "Sensor unavailable" }),
      expect.objectContaining({ value: "--", state: "muted", trend: "Sensor unavailable" })
    ]);
    expect(buildTrendChartData(merged, merged, bucketMs)[0]).toMatchObject({ pm2: 5, pm2Average: 5 });

    const restored = { ...good, timestamp: timestamp + 2000, pm2: 2 };
    const recovered = mergeReadingsForWindow([merged, [restored]], windowOption);
    expect(buildKpis(recovered, id)[0]).toMatchObject({ value: "2.0", state: "ok" });
    expect(recovered[0]).toMatchObject({ pmAvailable: true, pm2Max: 5 });
  });

  it("keeps a durable bucket's peak when its last unavailable sample is also in the live buffer", () => {
    const values = {
      timestamp: 1_800_000_000, temperature: 21, pressure: 1012, humidity: 48,
      oxidised: 1, reduced: 1, nh3: 1, pm1: 3, pm2: 6, pm10: 9
    };
    const history = normalizeReading({
      ...values, pm_available: true, latest_pm_available: false, pm2_max: 23
    });
    const live = normalizeReading({ ...values, pm_available: false, pm2: 99 });

    const windowOption = WINDOW_OPTIONS_BY_ID["7d"];
    const historyOnly = mergeReadingsForWindow([[history]], windowOption);
    const withOverlay = mergeReadingsForWindow([[history], [live]], windowOption);
    for (const merged of [historyOnly, withOverlay]) {
      expect(merged[0]).toMatchObject({ pmAvailable: false, pm2Max: 23 });
      expect(buildKpis(merged, "7d")[0]).toMatchObject({ value: "--", trend: "Sensor unavailable" });
      expect(buildTrendChartData(merged, merged, 60_000)[0]).toMatchObject({ pm2: 23, pm2Average: 6 });
    }
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
