import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import DashboardView from "./DashboardView";

vi.mock("recharts", () => ({
  CartesianGrid: () => null,
  Line: () => null,
  LineChart: ({ children }) => <svg aria-label="Chart">{children}</svg>,
  ResponsiveContainer: ({ children }) => <div>{children}</div>,
  Tooltip: () => null,
  XAxis: () => null,
  YAxis: () => null
}));

const windowOptions = [
  { id: "live", label: "Live" },
  { id: "1h", label: "1h" },
  { id: "24h", label: "24h" },
  { id: "7d", label: "7d" }
];

function renderDashboard(onSelectWindow = vi.fn()) {
  render(
    <DashboardView
      axisTickFormatter={(value) => value}
      chartData={[]}
      connectionStatus="live"
      feedError=""
      feedItems={[]}
      insightSource="openai"
      insights={[]}
      insightsError=""
      isLoadingFeed={false}
      isLoadingInsights={false}
      kpis={[
        { label: "PM2.5", value: "6.0", unit: "ug/m3", trend: "Stable", state: "ok" }
      ]}
      lastError=""
      lastReadingAt={1_800_000_000_000}
      onSelectWindow={onSelectWindow}
      selectedWindow={windowOptions[0]}
      temperatureDomain={[18, 26]}
      windowOptions={windowOptions}
    />
  );
  return onSelectWindow;
}

describe("DashboardView", () => {
  it("selects a requested time window", () => {
    const onSelectWindow = renderDashboard();

    fireEvent.click(screen.getByRole("tab", { name: "24h" }));

    expect(onSelectWindow).toHaveBeenCalledWith("24h");
  });

  it("shows connection and reading freshness", () => {
    renderDashboard();

    expect(screen.getByText("Connected")).toBeInTheDocument();
    expect(screen.getByText(/Last reading/)).toBeInTheDocument();
  });
});
