import { useEffect } from "react";
import { buildReadRequestOptions, parseJSONResponse } from "../lib/dashboardApi";
import { DASHBOARD_DEVICE_ID, LIVE_SOURCE_WINDOW_IDS } from "../lib/dashboardConfig";
import { buildHistoryUrl, mergeReadingsForWindow } from "../lib/dashboardTransforms";
import { normalizeReadings } from "../lib/readings";

const HISTORY_RETRY_MS = 60_000;

function buildLiveOverlayUrl(backendBaseUrl, windowOption) {
  const url = new URL(`${backendBaseUrl}/api/readings`);
  url.searchParams.set("limit", String(windowOption.queryMaxPoints));
  url.searchParams.set("source", "live");
  if (DASHBOARD_DEVICE_ID) {
    url.searchParams.set("device_id", DASHBOARD_DEVICE_ID);
  }
  return url.toString();
}

async function fetchWindowReadings(backendBaseUrl, windowOption, signal) {
  const historyUrl = buildHistoryUrl(backendBaseUrl, windowOption);
  const options = buildReadRequestOptions(signal);
  const response = await fetch(historyUrl, options);
  const payload = await parseJSONResponse(
    response,
    `${windowOption.label} history endpoint`,
    historyUrl,
    backendBaseUrl
  );
  if (!response.ok) {
    const message = typeof payload.error === "string"
      ? payload.error
      : `History request failed with status ${response.status}`;
    throw new Error(message);
  }

  const sources = [normalizeReadings(payload.readings)];
  if (!LIVE_SOURCE_WINDOW_IDS.has(windowOption.id) && !signal.aborted) {
    const liveUrl = buildLiveOverlayUrl(backendBaseUrl, windowOption);
    try {
      const liveResponse = await fetch(liveUrl, options);
      if (liveResponse.ok) {
        const livePayload = await parseJSONResponse(
          liveResponse,
          `${windowOption.label} live overlay endpoint`,
          liveUrl,
          backendBaseUrl
        );
        sources.push(normalizeReadings(livePayload.readings));
      }
    } catch (error) {
      if (!signal.aborted) {
        console.warn("Could not load the live overlay", error);
      }
    }
  }
  return sources;
}

export function useReadingsHistoryWindows({
  backendBaseUrl,
  historyCacheRef,
  latestLiveReadingAtRef,
  selectedWindow,
  syncConnectionStatus,
  setLastError,
  setReadings
}) {
  useEffect(() => {
    const { id, cacheTtlMs } = selectedWindow;
    const isLive = LIVE_SOURCE_WINDOW_IDS.has(id);
    const abortController = new AbortController();
    const { signal } = abortController;
    let timer = null;
    let loading = false;
    const cached = historyCacheRef.current[id];
    let nextFetchAt = cached?.fetchedAt == null ? 0 : cached.fetchedAt + cacheTtlMs;

    function display(readings) {
      setReadings(readings);
      if (isLive) {
        latestLiveReadingAtRef.current = readings.at(-1)?.timestamp ?? 0;
        syncConnectionStatus();
      }
    }

    async function refresh() {
      loading = true;
      try {
        const sources = await fetchWindowReadings(backendBaseUrl, selectedWindow, signal);
        if (signal.aborted) {
          return;
        }
        // The stream stays connected during HTTP requests. Keep samples that
        // arrived after the server took its snapshot.
        const readings = mergeReadingsForWindow(
          [historyCacheRef.current.live?.readings ?? [], ...sources],
          selectedWindow
        );
        const fetchedAt = Date.now();
        historyCacheRef.current[id] = { readings, fetchedAt };
        nextFetchAt = fetchedAt + cacheTtlMs;
        display(readings);
        setLastError("");
      } catch (error) {
        if (!signal.aborted) {
          setLastError(error instanceof Error ? error.message : "History request failed");
          nextFetchAt = Date.now() + Math.min(cacheTtlMs, HISTORY_RETRY_MS);
        }
      } finally {
        loading = false;
        if (!isLive && !signal.aborted) {
          schedule();
        }
      }
    }

    function schedule() {
      window.clearTimeout(timer);
      if (document.visibilityState === "hidden" || loading) {
        return;
      }
      const delay = nextFetchAt - Date.now();
      if (delay <= 0) {
        void refresh();
      } else if (!isLive) {
        timer = window.setTimeout(schedule, delay);
      }
    }

    display(cached?.readings ?? []);
    setLastError("");
    document.addEventListener("visibilitychange", schedule);
    schedule();

    return () => {
      abortController.abort();
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", schedule);
    };
  }, [
    backendBaseUrl,
    historyCacheRef,
    latestLiveReadingAtRef,
    selectedWindow,
    syncConnectionStatus,
    setLastError,
    setReadings
  ]);
}
