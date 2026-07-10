import { useEffect } from "react";
import { buildReadStreamUrl } from "../lib/dashboardApi";
import {
  CONNECTION_STATUS_CHECK_INTERVAL_MS,
  DASHBOARD_DEVICE_ID,
  STREAM_WINDOW_IDS,
  WINDOW_OPTIONS_BY_ID
} from "../lib/dashboardConfig";
import { appendReadingForWindow } from "../lib/dashboardTransforms";
import { normalizeReading } from "../lib/readings";

export function useReadingsStreamConnection({
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
