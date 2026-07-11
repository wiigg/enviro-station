import { describe, expect, it } from "vitest";
import { buildDashboardDiagnostics } from "./dashboardDiagnostics";

const now = 1_800_000_000_000;

function diagnostics(overrides = {}) {
  return buildDashboardDiagnostics({
    connectionStatus: "live",
    feedError: "",
    feedItems: [],
    insightGeneratedAt: now - 60_000,
    insightTrigger: "event",
    insights: [{ id: "insight-1" }],
    insightsError: "",
    isLoadingFeed: false,
    isLoadingInsights: false,
    lastError: "",
    lastReadingAt: now - 20_000,
    now,
    ...overrides
  });
}

describe("buildDashboardDiagnostics", () => {
  it("reports concise success when every end-to-end check passes", () => {
    const result = diagnostics();

    expect(result.summary).toBe("All checks passing");
    expect(result.tone).toBe("ok");
    expect(result.checks).toHaveLength(3);
    expect(result.checks.every((check) => check.state === "ok")).toBe(true);
    expect(result.checks.every((check) => check.action === "")).toBe(true);
  });

  it("gives a specific recovery action when telemetry is stale", () => {
    const result = diagnostics({
      connectionStatus: "offline",
      lastReadingAt: now - 4 * 60_000
    });
    const telemetry = result.checks.find((check) => check.id === "telemetry");

    expect(result.summary).toBe("1 action needed");
    expect(result.tone).toBe("error");
    expect(telemetry.summary).toContain("4m ago");
    expect(telemetry.action).toContain("sensor.service");
    expect(telemetry.action).toContain("Tailscale/Wi-Fi");
  });

  it("separates insight failure from live monitoring", () => {
    const result = diagnostics({ insightsError: "AI insights are currently unavailable." });
    const insights = result.checks.find((check) => check.id === "insights");

    expect(result.summary).toBe("1 check needs attention");
    expect(insights.summary).toContain("live monitoring is unaffected");
    expect(insights.action).toContain("backend AI logs");
  });

  it("flags insights that missed the scheduled safety refresh", () => {
    const result = diagnostics({ insightGeneratedAt: now - 8 * 60 * 60_000 });
    const insights = result.checks.find((check) => check.id === "insights");

    expect(insights.state).toBe("warn");
    expect(insights.summary).toContain("8h ago");
    expect(insights.action).toContain("six hours");
  });
});
