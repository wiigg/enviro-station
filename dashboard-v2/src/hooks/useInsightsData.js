import { useCallback, useState } from "react";
import { fetchEndpointJSON } from "../lib/dashboardApi";
import {
  INSIGHT_MAX_ITEMS,
  INSIGHT_POLL_INTERVAL_MS
} from "../lib/dashboardConfig";
import {
  normalizeInsight
} from "../lib/dashboardTransforms";
import { useSelfSchedulingPoll } from "./useSelfSchedulingPoll";

export function useInsightsData(backendBaseUrl) {
  const [insights, setInsights] = useState([]);
  const [insightsError, setInsightsError] = useState("");
  const [isLoadingInsights, setIsLoadingInsights] = useState(true);
  const [insightGeneratedAt, setInsightGeneratedAt] = useState(null);
  const [insightTrigger, setInsightTrigger] = useState("");

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
        const generatedAt = Number(payload.generated_at);
        const trigger = typeof payload.trigger === "string" ? payload.trigger : "";

        setInsights(nextInsights);
        setInsightGeneratedAt(Number.isFinite(generatedAt) ? generatedAt : null);
        setInsightTrigger(trigger);
        setInsightsError("");
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
    insightGeneratedAt,
    isLoadingInsights,
    insightTrigger
  };
}
