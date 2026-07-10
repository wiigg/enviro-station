import { describe, expect, it } from "vitest";
import { WINDOW_OPTIONS_BY_ID } from "./dashboardConfig";
import { buildHistoryUrl, downsampleReadings } from "./dashboardTransforms";

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

  it("builds bounded history requests for persisted windows", () => {
    const now = 1_800_000_000_000;
    const url = new URL(
      buildHistoryUrl("https://api.example.com", WINDOW_OPTIONS_BY_ID["24h"], now)
    );

    expect(url.pathname).toBe("/api/readings");
    expect(url.searchParams.get("to")).toBe(String(now));
    expect(url.searchParams.get("max_points")).toBe("960");
  });
});
