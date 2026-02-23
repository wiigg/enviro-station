import { useCallback, useMemo } from "react";
import { buildKpis } from "../lib/readings";
import { resolveBackendBaseUrl } from "../lib/dashboardApi";
import { WINDOW_OPTIONS } from "../lib/dashboardConfig";
import {
  computeTemperatureDomain,
  downsampleReadings
} from "../lib/dashboardTransforms";
import { useInsightsData } from "./useInsightsData";
import { useOpsFeedData } from "./useOpsFeedData";
import { useReadingsData } from "./useReadingsData";

export function useDashboardData() {
  const backendBaseUrl = useMemo(() => resolveBackendBaseUrl(), []);

  const {
    windowId,
    setWindowId,
    selectedWindow,
    readings,
    connectionStatus,
    lastError
  } = useReadingsData(backendBaseUrl);
  const {
    insights,
    insightsError,
    isLoadingInsights,
    insightSource
  } = useInsightsData(backendBaseUrl);
  const { feedItems, feedError, isLoadingFeed } = useOpsFeedData(backendBaseUrl);

  const chartReadings = useMemo(
    () => downsampleReadings(readings, selectedWindow.chartPoints),
    [readings, selectedWindow.chartPoints]
  );

  const kpis = useMemo(() => buildKpis(readings, windowId), [readings, windowId]);

  const chartData = useMemo(
    () =>
      chartReadings.map((reading) => ({
        timestamp: reading.timestamp,
        pm2: reading.pm2,
        temperature: reading.temperature
      })),
    [chartReadings]
  );

  const temperatureDomain = useMemo(
    () => computeTemperatureDomain(readings),
    [readings]
  );

  const axisTickFormatter = useCallback(
    (timestamp) => {
      const date = new Date(timestamp);
      if (Number.isNaN(date.getTime())) {
        return "";
      }

      if (windowId === "7d") {
        return date.toLocaleDateString([], { month: "short", day: "numeric" });
      }

      return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    },
    [windowId]
  );

  return {
    axisTickFormatter,
    chartData,
    connectionStatus,
    feedError,
    feedItems,
    insightSource,
    insights,
    insightsError,
    isLoadingFeed,
    isLoadingInsights,
    kpis,
    lastError,
    onSelectWindow: setWindowId,
    selectedWindow,
    temperatureDomain,
    windowOptions: WINDOW_OPTIONS
  };
}
