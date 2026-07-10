import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  LIVE_READING_STALE_AFTER_MS,
  WINDOW_OPTIONS,
  WINDOW_OPTIONS_BY_ID
} from "../lib/dashboardConfig";
import { useReadingsHistoryWindows } from "./useReadingsHistoryWindows";
import { useReadingsStreamConnection } from "./useReadingsStreamConnection";

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
