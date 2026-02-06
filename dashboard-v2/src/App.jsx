import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis
} from "recharts";
import {
  appendReading,
  buildKpis,
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

function formatChartLabel(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  });
}

function computeTemperatureDomain(readings) {
  if (!readings.length) {
    return [18, 26];
  }

  let min = readings[0].temperature;
  let max = readings[0].temperature;

  for (const reading of readings) {
    if (reading.temperature < min) {
      min = reading.temperature;
    }
    if (reading.temperature > max) {
      max = reading.temperature;
    }
  }

  const spread = Math.max(max - min, 1);
  const padding = Math.max(0.6, spread * 0.25);
  const lower = Math.floor((min - padding) * 10) / 10;
  const upper = Math.ceil((max + padding) * 10) / 10;
  return [lower, upper];
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

  const chartData = useMemo(
    () =>
      visibleReadings.map((reading) => ({
        timestamp: reading.timestamp,
        pm2: reading.pm2,
        temperature: reading.temperature
      })),
    [visibleReadings]
  );
  const temperatureDomain = useMemo(
    () => computeTemperatureDomain(visibleReadings),
    [visibleReadings]
  );
  const latestReadingTimeLabel = useMemo(() => {
    const latest = visibleReadings[visibleReadings.length - 1];
    if (!latest) {
      return "";
    }

    const date = new Date(latest.timestamp);
    if (Number.isNaN(date.getTime())) {
      return "";
    }

    return date.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit"
    });
  }, [visibleReadings]);

  const axisTickFormatter = useCallback(
    (timestamp) => {
      const date = new Date(timestamp);
      if (Number.isNaN(date.getTime())) {
        return "";
      }

      if (windowId === "7d") {
        return date.toLocaleDateString([], { month: "short", day: "numeric" });
      }

      return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    },
    [windowId]
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
              Phase 3 upgrades charts to a production-ready time-series engine.
            </p>
          </div>
          <div className="topbarMeta">
            <span className="chip chipPrimary">v2 Live</span>
            <span className={`chip chipStatus ${statusClassName(connectionStatus)}`}>
              {statusLabel(connectionStatus)}
            </span>
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
                : latestReadingTimeLabel
                  ? `Last reading ${latestReadingTimeLabel}`
                  : "Waiting for first reading"}
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
            {chartData.length ? (
              <div className="chart" role="img" aria-label="Particulate trend chart">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={chartData} margin={{ top: 8, right: 12, left: 0, bottom: 6 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(24, 35, 65, 0.12)" />
                    <XAxis
                      dataKey="timestamp"
                      tickFormatter={axisTickFormatter}
                      minTickGap={26}
                      tick={{ fill: "#5d6884", fontSize: 11 }}
                      axisLine={false}
                      tickLine={false}
                    />
                    <YAxis
                      width={44}
                      tick={{ fill: "#5d6884", fontSize: 11 }}
                      axisLine={false}
                      tickLine={false}
                    />
                    <Tooltip
                      labelFormatter={formatChartLabel}
                      formatter={(value) => [`${Number(value).toFixed(1)} ug/m3`, "PM2.5"]}
                      contentStyle={{
                        borderRadius: "12px",
                        border: "1px solid rgba(24, 35, 65, 0.12)",
                        background: "rgba(255,255,255,0.96)"
                      }}
                    />
                    <Line
                      type="monotone"
                      dataKey="pm2"
                      stroke="#f27a3e"
                      strokeWidth={3}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <p className="emptyState">No data in selected window yet.</p>
            )}
          </article>

          <article className="card panel">
            <div className="panelHead">
              <h2>Comfort Trend</h2>
              <span>Temperature over selected window</span>
            </div>
            {chartData.length ? (
              <div className="chart" role="img" aria-label="Comfort trend chart">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={chartData} margin={{ top: 8, right: 12, left: 0, bottom: 6 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(24, 35, 65, 0.12)" />
                    <XAxis
                      dataKey="timestamp"
                      tickFormatter={axisTickFormatter}
                      minTickGap={26}
                      tick={{ fill: "#5d6884", fontSize: 11 }}
                      axisLine={false}
                      tickLine={false}
                    />
                    <YAxis
                      domain={temperatureDomain}
                      width={44}
                      tick={{ fill: "#5d6884", fontSize: 11 }}
                      axisLine={false}
                      tickLine={false}
                    />
                    <Tooltip
                      labelFormatter={formatChartLabel}
                      formatter={(value) => [`${Number(value).toFixed(1)} C`, "Temperature"]}
                      contentStyle={{
                        borderRadius: "12px",
                        border: "1px solid rgba(24, 35, 65, 0.12)",
                        background: "rgba(255,255,255,0.96)"
                      }}
                    />
                    <Line
                      type="monotone"
                      dataKey="temperature"
                      stroke="#1f8a78"
                      strokeWidth={3}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
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
