import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { normalizeReading, normalizeReadings } from "../lib/readings";
import {
  buildReadRequestOptions,
  buildReadStreamUrl,
  parseJSONResponse
} from "../lib/dashboardApi";
import {
  CONNECTION_STATUS_CHECK_INTERVAL_MS,
  DASHBOARD_DEVICE_ID,
  LIVE_READING_STALE_AFTER_MS,
  LIVE_SOURCE_WINDOW_IDS,
  PREFETCH_WINDOW_IDS,
  STREAM_WINDOW_IDS,
  WINDOW_OPTIONS,
  WINDOW_OPTIONS_BY_ID
} from "../lib/dashboardConfig";
import {
  appendReadingForWindow,
  buildHistoryUrl,
  mergeReadingsForWindow
} from "../lib/dashboardTransforms";

function buildLiveOverlayUrl(backendBaseUrl, windowOption) {
  const url = new URL(`${backendBaseUrl}/api/readings`);
  url.searchParams.set("limit", String(windowOption.queryMaxPoints));
  url.searchParams.set("source", "live");
  if (DASHBOARD_DEVICE_ID) {
    url.searchParams.set("device_id", DASHBOARD_DEVICE_ID);
  }
  return url.toString();
}

function shouldOverlayLiveReadings(windowId) {
  return STREAM_WINDOW_IDS.includes(windowId) && !LIVE_SOURCE_WINDOW_IDS.has(windowId);
}

export function resolveConnectionStatus({
  isStreamConnected,
  latestReadingAt,
  previousStatus,
  now = Date.now()
}) {
  if (!isStreamConnected) {
    return previousStatus === "degraded" ? "degraded" : "connecting";
  }
  if (!latestReadingAt) {
    return "waiting";
  }
  return now - latestReadingAt <= LIVE_READING_STALE_AFTER_MS ? "live" : "offline";
}

function useReadingsHistoryWindows({
  backendBaseUrl,
  historyCacheRef,
  latestLiveReadingAtRef,
  selectedWindow,
  selectedWindowIdRef,
  syncConnectionStatus,
  setLastError,
  setReadings
}) {
  const loadReadingsWindow = useCallback(
    async (targetWindowId, signal) => {
      const targetWindow = WINDOW_OPTIONS_BY_ID[targetWindowId];
      const historyUrl = buildHistoryUrl(backendBaseUrl, targetWindow);
      const response = await fetch(historyUrl, buildReadRequestOptions(signal));
      const payload = await parseJSONResponse(
        response,
        `${targetWindow.label} history endpoint`,
        historyUrl,
        backendBaseUrl
      );

      if (!response.ok) {
        const errorMessage =
          typeof payload.error === "string"
            ? payload.error
            : `History request failed with status ${response.status}`;
        throw new Error(errorMessage);
      }

      let normalized = normalizeReadings(payload.readings);
      if (shouldOverlayLiveReadings(targetWindowId)) {
        const liveUrl = buildLiveOverlayUrl(backendBaseUrl, targetWindow);
        const liveResponse = await fetch(liveUrl, buildReadRequestOptions(signal));
        const livePayload = await parseJSONResponse(
          liveResponse,
          `${targetWindow.label} live overlay endpoint`,
          liveUrl,
          backendBaseUrl
        );

        if (liveResponse.ok) {
          normalized = mergeReadingsForWindow(
            [normalized, normalizeReadings(livePayload.readings)],
            targetWindow
          );
        }
      }
      historyCacheRef.current[targetWindowId] = {
        readings: normalized,
        fetchedAt: Date.now(),
        isHydrated: true
      };
      return normalized;
    },
    [backendBaseUrl, historyCacheRef]
  );

  useEffect(() => {
    let closed = false;
    const abortController = new AbortController();
    const cacheEntry = historyCacheRef.current[selectedWindow.id];

    if (cacheEntry) {
      setReadings(cacheEntry.readings);
      setLastError("");
      if (LIVE_SOURCE_WINDOW_IDS.has(selectedWindow.id)) {
        latestLiveReadingAtRef.current =
          cacheEntry.readings[cacheEntry.readings.length - 1]?.timestamp ?? 0;
        syncConnectionStatus();
      }
    } else {
      setReadings([]);
      if (LIVE_SOURCE_WINDOW_IDS.has(selectedWindow.id)) {
        latestLiveReadingAtRef.current = 0;
        syncConnectionStatus();
      }
    }

    const shouldRevalidate =
      !cacheEntry?.isHydrated ||
      Date.now() - cacheEntry.fetchedAt > selectedWindow.cacheTtlMs;
    if (!shouldRevalidate) {
      return () => {
        abortController.abort();
      };
    }

    async function revalidateSelectedWindow() {
      try {
        const normalized = await loadReadingsWindow(selectedWindow.id, abortController.signal);
        if (closed || abortController.signal.aborted) {
          return;
        }
        if (selectedWindowIdRef.current === selectedWindow.id) {
          setReadings(normalized);
        }
        if (LIVE_SOURCE_WINDOW_IDS.has(selectedWindow.id)) {
          latestLiveReadingAtRef.current =
            normalized[normalized.length - 1]?.timestamp ?? 0;
          syncConnectionStatus();
        }
        setLastError("");
      } catch (error) {
        if (closed || abortController.signal.aborted) {
          return;
        }
        const message = error instanceof Error ? error.message : "History request failed";
        setLastError(message);
      }
    }

    revalidateSelectedWindow();

    return () => {
      closed = true;
      abortController.abort();
    };
  }, [
    historyCacheRef,
    latestLiveReadingAtRef,
    loadReadingsWindow,
    selectedWindow.cacheTtlMs,
    selectedWindow.id,
    selectedWindowIdRef,
    syncConnectionStatus,
    setLastError,
    setReadings
  ]);

  useEffect(() => {
    if (LIVE_SOURCE_WINDOW_IDS.has(selectedWindow.id)) {
      return undefined;
    }

    let closed = false;
    let timerId = null;
    let activeAbortController = null;

    const schedule = () => {
      timerId = window.setTimeout(runRefresh, selectedWindow.cacheTtlMs);
    };

    async function runRefresh() {
      activeAbortController = new AbortController();

      try {
        const normalized = await loadReadingsWindow(
          selectedWindow.id,
          activeAbortController.signal
        );
        if (closed || activeAbortController.signal.aborted) {
          return;
        }
        if (selectedWindowIdRef.current === selectedWindow.id) {
          setReadings(normalized);
          setLastError("");
        }
      } catch (error) {
        if (closed || activeAbortController.signal.aborted) {
          return;
        }
        if (selectedWindowIdRef.current === selectedWindow.id) {
          const message = error instanceof Error ? error.message : "History request failed";
          setLastError(message);
        }
      } finally {
        activeAbortController = null;
        if (!closed) {
          schedule();
        }
      }
    }

    schedule();

    return () => {
      closed = true;
      if (timerId) {
        window.clearTimeout(timerId);
      }
      if (activeAbortController) {
        activeAbortController.abort();
      }
    };
  }, [
    loadReadingsWindow,
    selectedWindow.cacheTtlMs,
    selectedWindow.id,
    selectedWindowIdRef,
    setLastError,
    setReadings
  ]);

  useEffect(() => {
    let closed = false;
    const abortControllers = [];

    async function prefetchPrimaryWindows() {
      for (const targetWindowId of PREFETCH_WINDOW_IDS) {
        if (closed || targetWindowId === selectedWindow.id) {
          continue;
        }

        const targetWindow = WINDOW_OPTIONS_BY_ID[targetWindowId];
        const cacheEntry = historyCacheRef.current[targetWindowId];
        if (
          cacheEntry?.isHydrated &&
          Date.now() - cacheEntry.fetchedAt <= targetWindow.cacheTtlMs
        ) {
          continue;
        }

        const abortController = new AbortController();
        abortControllers.push(abortController);

        try {
          await loadReadingsWindow(targetWindowId, abortController.signal);
        } catch (_error) {
          if (closed || abortController.signal.aborted) {
            return;
          }
        }
      }
    }

    prefetchPrimaryWindows();

    return () => {
      closed = true;
      for (const abortController of abortControllers) {
        abortController.abort();
      }
    };
  }, [historyCacheRef, loadReadingsWindow, selectedWindow.id]);
}

function useReadingsStreamConnection({
  backendBaseUrl,
  historyCacheRef,
  latestLiveReadingAtRef,
  selectedWindowIdRef,
  streamConnectedRef,
  syncConnectionStatus,
  setConnectionStatus,
  setLastError,
  setReadings
}) {
  useEffect(() => {
    let closed = false;
    let eventSource = null;
    let reconnectTimeout = null;
    let retryDelayMs = 1000;

    const connect = () => {
      if (closed) {
        return;
      }

      streamConnectedRef.current = false;
      setConnectionStatus("connecting");
      const streamUrl = new URL(`${backendBaseUrl}/api/stream`);
      if (DASHBOARD_DEVICE_ID) {
        streamUrl.searchParams.set("device_id", DASHBOARD_DEVICE_ID);
      }
      eventSource = new EventSource(buildReadStreamUrl(streamUrl.toString()));

      const handleReading = (event) => {
        try {
          const parsed = JSON.parse(event.data);
          const reading = normalizeReading(parsed);
          if (!reading) {
            return;
          }
          if (DASHBOARD_DEVICE_ID && reading.deviceId !== DASHBOARD_DEVICE_ID) {
            return;
          }

          latestLiveReadingAtRef.current = reading.timestamp;
          const streamUpdatedAt = Date.now();

          for (const targetWindowId of STREAM_WINDOW_IDS) {
            const targetWindow = WINDOW_OPTIONS_BY_ID[targetWindowId];
            const cacheEntry = historyCacheRef.current[targetWindowId];
            const nextReadings = appendReadingForWindow(
              cacheEntry?.readings ?? [],
              reading,
              targetWindow
            );

            historyCacheRef.current[targetWindowId] = {
              readings: nextReadings,
              fetchedAt: streamUpdatedAt,
              isHydrated: cacheEntry?.isHydrated ?? false
            };

            if (selectedWindowIdRef.current === targetWindowId) {
              setReadings(nextReadings);
            }
          }

          setConnectionStatus("live");
          setLastError("");
        } catch (_error) {
          // Ignore malformed stream payloads and keep stream alive.
        }
      };

      eventSource.addEventListener("reading", handleReading);
      eventSource.onmessage = handleReading;

      eventSource.onopen = () => {
        streamConnectedRef.current = true;
        syncConnectionStatus();
        retryDelayMs = 1000;
      };

      eventSource.onerror = () => {
        if (closed) {
          return;
        }

        streamConnectedRef.current = false;
        setConnectionStatus("degraded");
        const reconnectDelay = retryDelayMs;
        retryDelayMs = Math.min(retryDelayMs * 2, 15000);

        eventSource.close();
        reconnectTimeout = setTimeout(connect, reconnectDelay);
      };
    };

    connect();

    return () => {
      closed = true;
      streamConnectedRef.current = false;
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
      }
      if (eventSource) {
        eventSource.close();
      }
    };
  }, [
    backendBaseUrl,
    historyCacheRef,
    latestLiveReadingAtRef,
    selectedWindowIdRef,
    streamConnectedRef,
    syncConnectionStatus,
    setConnectionStatus,
    setLastError,
    setReadings
  ]);

  useEffect(() => {
    const timer = setInterval(() => {
      syncConnectionStatus();
    }, CONNECTION_STATUS_CHECK_INTERVAL_MS);

    return () => {
      clearInterval(timer);
    };
  }, [syncConnectionStatus]);
}

export function useReadingsData(backendBaseUrl) {
  const [windowId, setWindowId] = useState("live");
  const [readings, setReadings] = useState([]);
  const [connectionStatus, setConnectionStatus] = useState("connecting");
  const [lastError, setLastError] = useState("");

  const historyCacheRef = useRef({});
  const latestLiveReadingAtRef = useRef(0);
  const selectedWindowIdRef = useRef(windowId);
  const streamConnectedRef = useRef(false);

  const selectedWindow = useMemo(
    () => WINDOW_OPTIONS_BY_ID[windowId] ?? WINDOW_OPTIONS[0],
    [windowId]
  );

  useEffect(() => {
    selectedWindowIdRef.current = windowId;
  }, [windowId]);

  const syncConnectionStatus = useCallback(() => {
    setConnectionStatus((previousStatus) =>
      resolveConnectionStatus({
        isStreamConnected: streamConnectedRef.current,
        latestReadingAt: latestLiveReadingAtRef.current,
        previousStatus
      })
    );
  }, []);

  useReadingsHistoryWindows({
    backendBaseUrl,
    historyCacheRef,
    latestLiveReadingAtRef,
    selectedWindow,
    selectedWindowIdRef,
    syncConnectionStatus,
    setLastError,
    setReadings
  });

  useReadingsStreamConnection({
    backendBaseUrl,
    historyCacheRef,
    latestLiveReadingAtRef,
    selectedWindowIdRef,
    streamConnectedRef,
    syncConnectionStatus,
    setConnectionStatus,
    setLastError,
    setReadings
  });

  return {
    windowId,
    setWindowId,
    selectedWindow,
    readings,
    connectionStatus,
    lastError
  };
}
