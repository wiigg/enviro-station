import { describe, expect, it } from "vitest";
import { buildKpis, normalizeReading } from "./readings";

function reading(overrides = {}) {
  return {
    timestamp: 1_800_000_000,
    temperature: 21,
    pressure: 1012,
    humidity: 48,
    oxidised: 1,
    reduced: 1,
    nh3: 1,
    pm1: 4,
    pm2: 6,
    pm10: 12,
    ...overrides
  };
}

describe("reading normalization and KPIs", () => {
  it("normalizes Unix seconds to milliseconds", () => {
    expect(normalizeReading(reading()).timestamp).toBe(1_800_000_000_000);
  });

  it("assigns particulate severity from the latest reading", () => {
    const normalized = [
      normalizeReading(reading({ timestamp: 1_800_000_000, pm2: 5 })),
      normalizeReading(reading({ timestamp: 1_800_000_010, pm2: 16 }))
    ];
    const pm25 = buildKpis(normalized).find((item) => item.label === "PM2.5");

    expect(pm25).toMatchObject({ value: "16.0", state: "alert" });
  });
});
