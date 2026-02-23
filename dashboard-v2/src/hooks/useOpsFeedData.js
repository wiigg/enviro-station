import { useCallback, useState } from "react";
import { fetchEndpointJSON } from "../lib/dashboardApi";
import {
  OPS_FEED_MAX_ITEMS,
  OPS_FEED_POLL_INTERVAL_MS
} from "../lib/dashboardConfig";
import { normalizeOpsEvent } from "../lib/dashboardTransforms";
import { useSelfSchedulingPoll } from "./useSelfSchedulingPoll";

export function useOpsFeedData(backendBaseUrl) {
  const [feedItems, setFeedItems] = useState([]);
  const [feedError, setFeedError] = useState("");
  const [isLoadingFeed, setIsLoadingFeed] = useState(true);

  const pollOpsFeed = useCallback(
    async ({ signal, isClosed }) => {
      try {
        const opsFeedUrl = `${backendBaseUrl}/api/ops/events?limit=${OPS_FEED_MAX_ITEMS}`;
        const payload = await fetchEndpointJSON({
          backendBaseUrl,
          endpointName: "Ops feed endpoint",
          requestUrl: opsFeedUrl,
          signal,
          unavailableMessage: "Ops log is currently unavailable.",
          warningLabel: "Ops feed"
        });

        if (isClosed()) {
          return;
        }

        const sourceData = Array.isArray(payload.events) ? payload.events : [];
        const normalized = sourceData.map(normalizeOpsEvent).filter(Boolean);
        setFeedItems(normalized.slice(0, OPS_FEED_MAX_ITEMS));
        setFeedError("");
      } catch (error) {
        if (isClosed() || signal.aborted) {
          return;
        }
        const diagnostic = error instanceof Error ? error.message : "failed to load ops feed";
        console.error("Ops feed fetch error", diagnostic);
        setFeedError("Ops log is currently unavailable.");
      } finally {
        if (!isClosed()) {
          setIsLoadingFeed(false);
        }
      }
    },
    [backendBaseUrl]
  );

  useSelfSchedulingPoll(pollOpsFeed, OPS_FEED_POLL_INTERVAL_MS);

  return {
    feedItems,
    feedError,
    isLoadingFeed
  };
}
