import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  appendReading,
  buildKpis,
  getSeries,
  normalizeReading,
  normalizeReadings
} from "./lib/readings";

const MAX_STREAM_POINTS = 10000;

const WINDOW_OPTIONS = [
  { id: "live", label: "Live", limit: 900 },
  { id: "1h", label: "1h", limit: 3600 },
  { id: "24h", label: "24h", limit: 10000 },
  { id: "7d", label: "7d", limit: 10000 }
];

function resolveBackendBaseUrl() {
  const env = import.meta.env.VITE_BACKEND_URL;
  if (env) {
    return env.replace(/\/$/, "");
  }

  if (typeof window === "undefined") {
    return "http://localhost:8080";
  }

  const { hostname, origin, protocol, port } = window.location;
  const isLocalHost = hostname === "localhost" || hostname === "127.0.0.1";
  if (isLocalHost && (port === "5173" || port === "4173")) {
    return `${protocol}//${hostname}:8080`;
  }

  return origin;
}

function pathFromSeries(series, width, height, inset) {
  if (!series.length) {
    return "";
  }

  if (series.length === 1) {
    const centerY = height / 2;
    return `M${inset},${centerY} L${width - inset},${centerY}`;
  }

  const min = Math.min(...series);
  const max = Math.max(...series);
  const range = Math.max(1, max - min);
  const step = (width - inset * 2) / (series.length - 1);

  return series
    .map((value, index) => {
      const x = inset + index * step;
      const y = height - inset - ((value - min) / range) * (height - inset * 2);
      return `${index === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(" ");
}

function buildFeedItem(title, detail) {
  return {
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    time: new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
    title,
    detail
  };
}

function statusLabel(status) {
  if (status === "live") {
    return "Stream Live";
  }
  if (status === "degraded") {
    return "Reconnecting";
  }
  if (status === "offline") {
    return "Offline";
  }
  return "Connecting";
}

function statusClassName(status) {
  if (status === "live") {
    return "statusLive";
  }
  if (status === "degraded") {
    return "statusDegraded";
  }
  if (status === "offline") {
    return "statusOffline";
  }
  return "statusConnecting";
}

export default function App() {
  const backendBaseUrl = useMemo(() => resolveBackendBaseUrl(), []);

  const [windowId, setWindowId] = useState("live");
  const [readings, setReadings] = useState([]);
  const [connectionStatus, setConnectionStatus] = useState("connecting");
  const [isLoading, setIsLoading] = useState(true);
  const [lastError, setLastError] = useState("");
  const [feedItems, setFeedItems] = useState([
    buildFeedItem("Dashboard started", "Waiting for backend history and realtime stream")
  ]);

  const lastStreamEventAtRef = useRef(0);
  const streamOpenedRef = useRef(false);

  const selectedWindow = useMemo(
    () => WINDOW_OPTIONS.find((windowOption) => windowOption.id === windowId) ?? WINDOW_OPTIONS[0],
    [windowId]
  );

  const addFeedItem = useCallback((title, detail) => {
    setFeedItems((previousItems) => [buildFeedItem(title, detail), ...previousItems].slice(0, 8));
  }, []);

  useEffect(() => {
    const abortController = new AbortController();

    async function loadHistory() {
      setIsLoading(true);

      try {
        const historyUrl = `${backendBaseUrl}/api/readings?limit=${selectedWindow.limit}`;
        const response = await fetch(historyUrl, { signal: abortController.signal });

        if (!response.ok) {
          throw new Error(`History request failed with status ${response.status}`);
        }

        const payloadText = await response.text();
        let payload;
        try {
          payload = JSON.parse(payloadText);
        } catch (_error) {
          throw new Error(
            `History endpoint returned non-JSON. Check VITE_BACKEND_URL (${backendBaseUrl}).`
          );
        }

        const normalized = normalizeReadings(payload.readings);
        setReadings(normalized);
        setLastError("");
        addFeedItem(
          "History synced",
          `${normalized.length} readings loaded for ${selectedWindow.label} window`
        );
      } catch (error) {
        if (abortController.signal.aborted) {
          return;
        }

        const message = error instanceof Error ? error.message : "History request failed";
        setLastError(message);
        addFeedItem("History load failed", message);
      } finally {
        if (!abortController.signal.aborted) {
          setIsLoading(false);
        }
      }
    }

    loadHistory();

    return () => {
      abortController.abort();
    };
  }, [addFeedItem, backendBaseUrl, selectedWindow.id, selectedWindow.label, selectedWindow.limit]);

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
      eventSource = new EventSource(`${backendBaseUrl}/api/stream`);

      const handleReading = (event) => {
        try {
          const parsed = JSON.parse(event.data);
          const reading = normalizeReading(parsed);
          if (!reading) {
            return;
          }

          lastStreamEventAtRef.current = Date.now();
          setReadings((previousReadings) =>
            appendReading(previousReadings, reading, MAX_STREAM_POINTS)
          );
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
        if (!streamOpenedRef.current) {
          addFeedItem("Stream connected", "Realtime updates are active");
          streamOpenedRef.current = true;
        }
      };

      eventSource.onerror = () => {
        if (closed) {
          return;
        }

        setConnectionStatus("degraded");
        const reconnectDelay = retryDelayMs;
        retryDelayMs = Math.min(retryDelayMs * 2, 15000);
        addFeedItem("Stream interrupted", `Retrying in ${Math.round(reconnectDelay / 1000)}s`);

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
  }, [addFeedItem, backendBaseUrl]);

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
  }, []);

  const visibleReadings = useMemo(() => {
    if (readings.length <= selectedWindow.limit) {
      return readings;
    }
    return readings.slice(readings.length - selectedWindow.limit);
  }, [readings, selectedWindow.limit]);

  const kpis = useMemo(() => buildKpis(visibleReadings), [visibleReadings]);

  const series = useMemo(() => getSeries(visibleReadings), [visibleReadings]);
  const particulatePath = useMemo(
    () => pathFromSeries(series.particulate, 560, 190, 14),
    [series.particulate]
  );
  const comfortPath = useMemo(
    () => pathFromSeries(series.comfort, 560, 190, 14),
    [series.comfort]
  );

  return (
    <div className="shell">
      <div className="background" aria-hidden="true" />
      <main className="layout">
        <header className="card topbar reveal">
          <div>
            <p className="eyebrow">Enviro Station</p>
            <h1>Air Quality Control Deck</h1>
            <p className="subtitle">
              Phase 2 enables live backend integration via history bootstrap and SSE stream.
            </p>
          </div>
          <div className="topbarMeta">
            <span className="chip chipPrimary">v2 Live</span>
            <span className={`chip chipStatus ${statusClassName(connectionStatus)}`}>
              {statusLabel(connectionStatus)}
            </span>
            <span className="chip">{visibleReadings.length} points</span>
          </div>
        </header>

        <section className="card controls reveal">
          <div className="controlGroup">
            {WINDOW_OPTIONS.map((windowOption) => (
              <button
                key={windowOption.id}
                className={`btn ${windowOption.id === selectedWindow.id ? "btnActive" : ""}`}
                type="button"
                onClick={() => setWindowId(windowOption.id)}
              >
                {windowOption.label}
              </button>
            ))}
          </div>
          <p className={`hint ${lastError ? "hintError" : ""}`}>
            {isLoading
              ? "Loading history..."
              : lastError
                ? lastError
                : `Connected to ${backendBaseUrl}`}
          </p>
        </section>

        <section className="kpiGrid reveal">
          {kpis.map((item) => (
            <article className="card kpi" key={item.label}>
              <div className="kpiHead">
                <span>{item.label}</span>
                <span className={`dot ${item.state}`} />
              </div>
              <p className="kpiValue">
                {item.value}
                <span>{item.unit}</span>
              </p>
              <p className="kpiTrend">{item.trend}</p>
            </article>
          ))}
        </section>

        <section className="dataGrid reveal">
          <article className="card panel">
            <div className="panelHead">
              <h2>Particulate Trend</h2>
              <span>PM2.5 over selected window</span>
            </div>
            {series.particulate.length ? (
              <svg
                viewBox="0 0 560 190"
                className="chart"
                role="img"
                aria-label="Particulate trend chart"
              >
                <path d={particulatePath} className="line lineHot" />
              </svg>
            ) : (
              <p className="emptyState">No data in selected window yet.</p>
            )}
          </article>

          <article className="card panel">
            <div className="panelHead">
              <h2>Comfort Trend</h2>
              <span>Temperature over selected window</span>
            </div>
            {series.comfort.length ? (
              <svg
                viewBox="0 0 560 190"
                className="chart"
                role="img"
                aria-label="Comfort trend chart"
              >
                <path d={comfortPath} className="line lineCool" />
              </svg>
            ) : (
              <p className="emptyState">No data in selected window yet.</p>
            )}
          </article>

          <aside className="card panel feed">
            <div className="panelHead">
              <h2>Ops Feed</h2>
              <span>History + stream events</span>
            </div>
            <ul>
              {feedItems.map((incident) => (
                <li key={incident.id}>
                  <p className="time">{incident.time}</p>
                  <div>
                    <p className="event">{incident.title}</p>
                    <p className="detail">{incident.detail}</p>
                  </div>
                </li>
              ))}
            </ul>
          </aside>
        </section>
      </main>
    </div>
  );
}
