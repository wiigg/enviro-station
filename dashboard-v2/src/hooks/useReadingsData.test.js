import { describe, expect, it } from "vitest";
import { resolveConnectionStatus } from "./useReadingsData";

const NOW = 1_800_000_000_000;

describe("resolveConnectionStatus", () => {
  it("distinguishes fresh, stale, and missing readings", () => {
    expect(
      resolveConnectionStatus({
        isStreamConnected: true,
        latestReadingAt: NOW - 10_000,
        previousStatus: "connecting",
        now: NOW
      })
    ).toBe("live");

    expect(
      resolveConnectionStatus({
        isStreamConnected: true,
        latestReadingAt: NOW - 60_000,
        previousStatus: "live",
        now: NOW
      })
    ).toBe("offline");

    expect(
      resolveConnectionStatus({
        isStreamConnected: true,
        latestReadingAt: 0,
        previousStatus: "connecting",
        now: NOW
      })
    ).toBe("waiting");
  });

  it("preserves degraded while a stream is disconnected", () => {
    expect(
      resolveConnectionStatus({
        isStreamConnected: false,
        latestReadingAt: NOW,
        previousStatus: "degraded",
        now: NOW
      })
    ).toBe("degraded");
  });
});
