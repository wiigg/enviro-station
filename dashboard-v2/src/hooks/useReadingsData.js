import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { normalizeReading, normalizeReadings } from "../lib/readings";
import { parseJSONResponse } from "../lib/dashboardApi";
import {
  PREFETCH_WINDOW_IDS,
  STREAM_WINDOW_IDS,
  WINDOW_OPTIONS,
  WINDOW_OPTIONS_BY_ID
} from "../lib/dashboardConfig";
import {
  appendReadingForWindow,
  buildHistoryUrl
} from "../lib/dashboardTransforms";

function useReadingsHistoryWindows({
  backendBaseUrl,
  historyCacheRef,
  selectedWindow,
  selectedWindowIdRef,
  setLastError,
  setReadings
}) {
  const loadReadingsWindow = useCallback(
    async (targetWindowId, signal) => {
      const targetWindow = WINDOW_OPTIONS_BY_ID[targetWindowId];
      const historyUrl = buildHistoryUrl(backendBaseUrl, targetWindow);
      const response = await fetch(historyUrl, { signal });
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

      const normalized = normalizeReadings(payload.readings);
      historyCacheRef.current[targetWindowId] = {
        readings: normalized,
        fetchedAt: Date.now()
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
    } else {
      setReadings([]);
    }

    const shouldRevalidate =
      !cacheEntry || Date.now() - cacheEntry.fetchedAt > selectedWindow.cacheTtlMs;
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
        if (cacheEntry && Date.now() - cacheEntry.fetchedAt <= targetWindow.cacheTtlMs) {
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
  selectedWindowIdRef,
  setConnectionStatus,
  setLastError,
  setReadings
}) {
  const lastStreamEventAtRef = useRef(0);

  useEffect(() => {
    let closed = false;
    let eventSource = null;
    let reconnectTimeout = null;
    let retryDelayMs = 1000;

    const connect = () => {
      if (closed) {
        return;
      }

      setConnectionStatus("connecting");
      const streamUrl = new URL(`${backendBaseUrl}/api/stream`);
      eventSource = new EventSource(streamUrl.toString());

      const handleReading = (event) => {
        try {
          const parsed = JSON.parse(event.data);
          const reading = normalizeReading(parsed);
          if (!reading) {
            return;
          }

          lastStreamEventAtRef.current = Date.now();
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
              fetchedAt: streamUpdatedAt
            };

            if (selectedWindowIdRef.current === targetWindowId) {
              setReadings(nextReadings);
            }
          }

          if (selectedWindowIdRef.current === "7d") {
            const cacheEntry = historyCacheRef.current["7d"];
            if (cacheEntry?.readings?.length) {
              const nextReadings = appendReadingForWindow(
                cacheEntry.readings,
                reading,
                WINDOW_OPTIONS_BY_ID["7d"]
              );
              historyCacheRef.current["7d"] = {
                readings: nextReadings,
                fetchedAt: streamUpdatedAt
              };
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
        setConnectionStatus("live");
        retryDelayMs = 1000;
      };

      eventSource.onerror = () => {
        if (closed) {
          return;
        }

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
    selectedWindowIdRef,
    setConnectionStatus,
    setLastError,
    setReadings
  ]);

  useEffect(() => {
    const timer = setInterval(() => {
      if (!lastStreamEventAtRef.current) {
        return;
      }

      if (Date.now() - lastStreamEventAtRef.current > 45000) {
        setConnectionStatus((previousStatus) =>
          previousStatus === "live" ? "offline" : previousStatus
        );
      }
    }, 5000);

    return () => {
      clearInterval(timer);
    };
  }, [setConnectionStatus]);
}

export function useReadingsData(backendBaseUrl) {
  const [windowId, setWindowId] = useState("live");
  const [readings, setReadings] = useState([]);
  const [connectionStatus, setConnectionStatus] = useState("connecting");
  const [lastError, setLastError] = useState("");

  const historyCacheRef = useRef({});
  const selectedWindowIdRef = useRef(windowId);

  const selectedWindow = useMemo(
    () => WINDOW_OPTIONS_BY_ID[windowId] ?? WINDOW_OPTIONS[0],
    [windowId]
  );

  useEffect(() => {
    selectedWindowIdRef.current = windowId;
  }, [windowId]);

  useReadingsHistoryWindows({
    backendBaseUrl,
    historyCacheRef,
    selectedWindow,
    selectedWindowIdRef,
    setLastError,
    setReadings
  });

  useReadingsStreamConnection({
    backendBaseUrl,
    historyCacheRef,
    selectedWindowIdRef,
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
