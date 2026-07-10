import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import DashboardView from "./DashboardView";

vi.mock("recharts", () => ({
  CartesianGrid: () => null,
  Line: () => null,
  LineChart: ({ children }) => <svg aria-label="Chart">{children}</svg>,
  ResponsiveContainer: ({ children }) => <div>{children}</div>,
  Tooltip: () => null,
  XAxis: ({ type, scale }) => (
    <g data-testid="x-axis" data-axis-type={type} data-axis-scale={scale} />
  ),
  YAxis: () => null
}));

const windowOptions = [
  { id: "live", label: "Live" },
  { id: "1h", label: "1h" },
  { id: "24h", label: "24h" },
  { id: "7d", label: "7d" }
];

function renderDashboard(onSelectWindow = vi.fn(), chartData = []) {
  render(
    <DashboardView
      axisTickFormatter={(value) => value}
      chartData={chartData}
      connectionStatus="live"
      deviceLabel="Office"
      feedError=""
      feedItems={[]}
      insightSource="openai"
      insights={[]}
      insightsError=""
      isLoadingFeed={false}
      isLoadingInsights={false}
      kpis={[
        { label: "PM2.5", value: "6.0", unit: "µg/m³", trend: "Stable", state: "ok" }
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

    fireEvent.click(screen.getByRole("button", { name: "24h" }));

    expect(onSelectWindow).toHaveBeenCalledWith("24h");
  });

  it("shows connection and reading freshness", () => {
    renderDashboard();

    expect(screen.getByRole("status")).toHaveTextContent("Connected");
    expect(screen.getByRole("group", { name: "Time range" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Live" })).toHaveAttribute(
      "aria-pressed",
      "true"
    );
    expect(screen.getByText(/Last reading/)).toBeInTheDocument();
    expect(screen.getByText("Enviro Station · Office")).toBeInTheDocument();
    expect(screen.getByText("Diagnostics").closest("details")).not.toHaveAttribute("open");
  });

  it("uses elapsed time for chart spacing", () => {
    renderDashboard(vi.fn(), [
      {
        timestamp: 1_800_000_000_000,
        pm2: 5,
        pm2Average: 5,
        temperature: 20,
        temperatureAverage: 20
      }
    ]);

    for (const axis of screen.getAllByTestId("x-axis")) {
      expect(axis).toHaveAttribute("data-axis-type", "number");
      expect(axis).toHaveAttribute("data-axis-scale", "time");
    }
  });
});
