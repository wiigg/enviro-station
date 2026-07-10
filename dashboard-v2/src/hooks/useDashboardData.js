import { useCallback, useMemo } from "react";
import { buildKpis } from "../lib/readings";
import { resolveBackendBaseUrl } from "../lib/dashboardApi";
import { WINDOW_OPTIONS } from "../lib/dashboardConfig";
import {
  buildTrendChartData,
  computeTemperatureDomain,
  filterVisibleReadings,
  downsampleReadings,
  normalizeInsightTextForSeverity
} from "../lib/dashboardTransforms";
import { useInsightsData } from "./useInsightsData";
import { useOpsFeedData } from "./useOpsFeedData";
import { useReadingsData } from "./useReadingsData";

function strongestState(states) {
  if (states.includes("alert")) {
    return "alert";
  }
  if (states.includes("warn")) {
    return "warn";
  }
  if (states.includes("ok")) {
    return "ok";
  }
  return "muted";
}

function insightTopicState(insight, kpis) {
  if (insight.topic === "humidity") {
    return kpis.find((item) => item.label === "Humidity")?.state ?? "muted";
  }
  if (insight.topic === "temperature") {
    return kpis.find((item) => item.label === "Temp")?.state ?? "muted";
  }
  if (insight.topic === "air_quality") {
    return strongestState(
      kpis
        .filter((item) => item.label === "PM2.5" || item.label === "PM10")
        .map((item) => item.state)
    );
  }
  return "";
}

function alignInsightSeverity(insight, kpis) {
  const topicState = insightTopicState(insight, kpis);
  if (topicState === "alert" && insight.severity !== "critical") {
    return { ...insight, kind: "alert", severity: "critical" };
  }
  if (topicState === "warn" && insight.severity === "critical") {
    return {
      ...insight,
      title: normalizeInsightTextForSeverity(insight.title, "warn"),
      message: normalizeInsightTextForSeverity(insight.message, "warn"),
      severity: "warn"
    };
  }
  return insight;
}

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

  const visibleReadings = useMemo(
    () => filterVisibleReadings(readings, selectedWindow.rangeMs),
    [readings, selectedWindow.rangeMs]
  );

  const chartReadings = useMemo(
    () => downsampleReadings(visibleReadings, selectedWindow.chartPoints),
    [visibleReadings, selectedWindow.chartPoints]
  );

  const kpis = useMemo(() => buildKpis(readings, windowId), [readings, windowId]);
  const lastReadingAt = readings[readings.length - 1]?.timestamp ?? null;

  const alignedInsights = useMemo(
    () => insights.map((insight) => alignInsightSeverity(insight, kpis)),
    [insights, kpis]
  );

  const chartData = useMemo(
    () =>
      buildTrendChartData(
        visibleReadings,
        chartReadings,
        selectedWindow.trendAverageWindowMs
      ),
    [chartReadings, selectedWindow.trendAverageWindowMs, visibleReadings]
  );

  const temperatureDomain = useMemo(
    () => computeTemperatureDomain(visibleReadings),
    [visibleReadings]
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
    insights: alignedInsights,
    insightsError,
    isLoadingFeed,
    isLoadingInsights,
    kpis,
    lastError,
    lastReadingAt,
    onSelectWindow: setWindowId,
    selectedWindow,
    temperatureDomain,
    windowOptions: WINDOW_OPTIONS
  };
}
