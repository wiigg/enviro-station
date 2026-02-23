import { useCallback, useMemo, useState } from "react";
import { fetchEndpointJSON } from "../lib/dashboardApi";
import {
  INSIGHT_MAX_ITEMS,
  INSIGHT_POLL_INTERVAL_MS
} from "../lib/dashboardConfig";
import {
  readInsightsCache,
  writeInsightsCache
} from "../lib/dashboardInsightsCache";
import {
  normalizeInsight
} from "../lib/dashboardTransforms";
import { useSelfSchedulingPoll } from "./useSelfSchedulingPoll";

export function useInsightsData(backendBaseUrl) {
  const initialInsightsCache = useMemo(
    () => readInsightsCache(backendBaseUrl),
    [backendBaseUrl]
  );

  const [insights, setInsights] = useState(initialInsightsCache?.insights ?? []);
  const [insightsError, setInsightsError] = useState("");
  const [isLoadingInsights, setIsLoadingInsights] = useState(
    (initialInsightsCache?.insights?.length ?? 0) === 0
  );
  const [insightSource, setInsightSource] = useState(initialInsightsCache?.source ?? "openai");

  const pollInsights = useCallback(
    async ({ signal, isClosed }) => {
      try {
        const insightsUrl = `${backendBaseUrl}/api/insights?limit=${INSIGHT_MAX_ITEMS}`;
        const payload = await fetchEndpointJSON({
          backendBaseUrl,
          endpointName: "Insights endpoint",
          requestUrl: insightsUrl,
          signal,
          unavailableMessage: "AI insights are currently unavailable.",
          warningLabel: "Insights"
        });

        if (isClosed()) {
          return;
        }

        const sourceData = Array.isArray(payload.insights) ? payload.insights : [];
        const nextInsights = sourceData
          .map(normalizeInsight)
          .filter(Boolean)
          .slice(0, INSIGHT_MAX_ITEMS);
        const nextSource = typeof payload.source === "string" ? payload.source : "openai";

        setInsights(nextInsights);
        setInsightSource(nextSource);
        setInsightsError("");
        writeInsightsCache(backendBaseUrl, nextSource, nextInsights);
      } catch (error) {
        if (isClosed() || signal.aborted) {
          return;
        }
        const diagnostic = error instanceof Error ? error.message : "failed to load insights";
        console.error("Insights fetch error", diagnostic);
        setInsightsError("AI insights are currently unavailable.");
      } finally {
        if (!isClosed()) {
          setIsLoadingInsights(false);
        }
      }
    },
    [backendBaseUrl]
  );

  useSelfSchedulingPoll(pollInsights, INSIGHT_POLL_INTERVAL_MS);

  return {
    insights,
    insightsError,
    isLoadingInsights,
    insightSource
  };
}
