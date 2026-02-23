import { INSIGHT_MAX_ITEMS } from "./dashboardConfig";
import { normalizeInsight } from "./dashboardTransforms";

const INSIGHT_CACHE_KEY = "envirostation.insights.v1";

export function readInsightsCache(backendBaseUrl) {
  if (typeof window === "undefined") {
    return null;
  }

  try {
    const cachedRaw = window.sessionStorage.getItem(INSIGHT_CACHE_KEY);
    if (!cachedRaw) {
      return null;
    }

    const cached = JSON.parse(cachedRaw);
    if (!cached || typeof cached !== "object") {
      return null;
    }

    const cachedBackendBaseUrl =
      typeof cached.backend_base_url === "string" ? cached.backend_base_url : "";
    if (cachedBackendBaseUrl !== backendBaseUrl) {
      return null;
    }

    const cachedSource = typeof cached.source === "string" ? cached.source : "openai";
    const cachedInsightsRaw = Array.isArray(cached.insights) ? cached.insights : [];
    const cachedInsights = cachedInsightsRaw
      .map(normalizeInsight)
      .filter(Boolean)
      .slice(0, INSIGHT_MAX_ITEMS);

    return {
      source: cachedSource,
      insights: cachedInsights
    };
  } catch (_error) {
    return null;
  }
}

export function writeInsightsCache(backendBaseUrl, source, insights) {
  if (typeof window === "undefined") {
    return;
  }

  try {
    window.sessionStorage.setItem(
      INSIGHT_CACHE_KEY,
      JSON.stringify({
        backend_base_url: backendBaseUrl,
        source,
        insights
      })
    );
  } catch (_error) {
    // Ignore cache write failures.
  }
}
