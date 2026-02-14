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

const MAX_STREAM_POINTS = 100000;

const WINDOW_OPTIONS = [
  { id: "live", label: "Live", historyLimit: 900, chartPoints: 900 },
  { id: "1h", label: "1h", historyLimit: 3600, chartPoints: 1800 },
  { id: "24h", label: "24h", historyLimit: 86400, chartPoints: 7200 },
  { id: "7d", label: "7d", historyLimit: 100000, chartPoints: 7000 }
];

const INSIGHT_POLL_INTERVAL_MS = 30000;
const INSIGHT_MAX_ITEMS = 3;
const INSIGHT_CACHE_KEY = "envirostation.insights.v1";
const OPS_FEED_POLL_INTERVAL_MS = 15000;
const OPS_FEED_MAX_ITEMS = 6;

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

  if (hostname === "envirostation-dashboard.fly.dev") {
    return "https://envirostation-api.fly.dev";
  }

  return origin;
}

function compactText(value) {
  return value.replace(/\s+/g, " ").trim();
}

async function parseJSONResponse(response, endpointName, requestUrl, backendBaseUrl) {
  const payloadText = await response.text();
  if (!payloadText) {
    return {};
  }

  try {
    return JSON.parse(payloadText);
  } catch (_error) {
    const compactPayload = compactText(payloadText);
    const contentType = (response.headers.get("content-type") || "").toLowerCase();
    const looksHTML =
      contentType.includes("text/html") ||
      compactPayload.toLowerCase().startsWith("<!doctype html") ||
      compactPayload.toLowerCase().startsWith("<html");

    if (looksHTML) {
      throw new Error(
        `${endpointName} returned HTML. Check VITE_BACKEND_URL (${backendBaseUrl}) and verify ${requestUrl} points to the backend API.`
      );
    }

    const preview = compactPayload.slice(0, 140);
    throw new Error(
      `${endpointName} returned non-JSON${preview ? `: ${preview}` : ""}`
    );
  }
}

function formatOpsEventTime(timestamp) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function normalizeOpsEvent(rawEvent) {
  if (!rawEvent || typeof rawEvent !== "object") {
    return null;
  }

  const title = typeof rawEvent.title === "string" ? rawEvent.title.trim() : "";
  const detail = typeof rawEvent.detail === "string" ? rawEvent.detail.trim() : "";
  const timestampRaw =
    typeof rawEvent.timestamp === "number" ? rawEvent.timestamp : Number(rawEvent.timestamp);
  if (!title || !detail || !Number.isFinite(timestampRaw)) {
    return null;
  }

  const idRaw = rawEvent.id;
  const id =
    typeof idRaw === "number" || typeof idRaw === "string"
      ? String(idRaw)
      : `${timestampRaw}-${title}-${detail}`.toLowerCase();

  return {
    id,
    time: formatOpsEventTime(timestampRaw),
    title,
    detail
  };
}

function statusLabel(status) {
  if (status === "live") {
    return "Connected";
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

function downsampleReadings(readings, maxPoints) {
  if (!Array.isArray(readings) || readings.length <= maxPoints) {
    return readings;
  }

  const stride = Math.ceil(readings.length / maxPoints);
  return readings.filter((_, index) => index % stride === 0);
}

function normalizeInsight(rawInsight) {
  if (!rawInsight || typeof rawInsight !== "object") {
    return null;
  }

  const title = typeof rawInsight.title === "string" ? rawInsight.title.trim() : "";
  const message = typeof rawInsight.message === "string" ? rawInsight.message.trim() : "";
  const severityRaw = typeof rawInsight.severity === "string" ? rawInsight.severity.trim() : "";
  const kindRaw = typeof rawInsight.kind === "string" ? rawInsight.kind.trim() : "";
  const severity = severityRaw.toLowerCase();
  const kind = kindRaw.toLowerCase();
  const normalizedSeverity =
    severity === "critical" || severity === "warn" || severity === "info" ? severity : "info";
  const normalizedKind = kind === "alert" || kind === "insight" || kind === "tip" ? kind : "insight";

  if (!title || !message) {
    return null;
  }

  return {
    id: `${normalizedKind}-${normalizedSeverity}-${title}-${message}`.toLowerCase(),
    title,
    message,
    severity: normalizedSeverity,
    kind: normalizedKind
  };
}

function readInsightsCache(backendBaseUrl) {
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

function writeInsightsCache(backendBaseUrl, source, generatedAt, insights) {
  if (typeof window === "undefined") {
    return;
  }

  try {
    window.sessionStorage.setItem(
      INSIGHT_CACHE_KEY,
      JSON.stringify({
        backend_base_url: backendBaseUrl,
        source,
        generated_at: generatedAt,
        insights
      })
    );
  } catch (_error) {
    // Ignore cache write failures.
  }
}

function insightSeverityClassName(severity) {
  if (severity === "critical") {
    return "insightSeverityCritical";
  }
  if (severity === "warn") {
    return "insightSeverityWarn";
  }
  return "insightSeverityInfo";
}

function insightSeverityLabel(severity) {
  if (severity === "critical") {
    return "Critical";
  }
  if (severity === "warn") {
    return "Warn";
  }
  return "Info";
}

function insightKindLabel(kind) {
  if (kind === "alert") {
    return "Alert";
  }
  if (kind === "tip") {
    return "Tip";
  }
  return "Insight";
}

export default function App() {
  const backendBaseUrl = useMemo(() => resolveBackendBaseUrl(), []);
  const initialInsightsCache = useMemo(
    () => readInsightsCache(backendBaseUrl),
    [backendBaseUrl]
  );

  const [windowId, setWindowId] = useState("live");
  const [readings, setReadings] = useState([]);
  const [connectionStatus, setConnectionStatus] = useState("connecting");
  const [isLoadingHistory, setIsLoadingHistory] = useState(true);
  const [lastError, setLastError] = useState("");
  const [insights, setInsights] = useState(initialInsightsCache?.insights ?? []);
  const [insightsError, setInsightsError] = useState("");
  const [isLoadingInsights, setIsLoadingInsights] = useState(
    (initialInsightsCache?.insights?.length ?? 0) === 0
  );
  const [insightSource, setInsightSource] = useState(initialInsightsCache?.source ?? "openai");
  const [feedItems, setFeedItems] = useState([]);
  const [feedError, setFeedError] = useState("");
  const [isLoadingFeed, setIsLoadingFeed] = useState(true);

  const lastStreamEventAtRef = useRef(0);

  const selectedWindow = useMemo(
    () => WINDOW_OPTIONS.find((windowOption) => windowOption.id === windowId) ?? WINDOW_OPTIONS[0],
    [windowId]
  );

  useEffect(() => {
    const abortController = new AbortController();

    async function loadHistory() {
      setIsLoadingHistory(true);

      try {
        const historyUrl = `${backendBaseUrl}/api/readings?limit=${selectedWindow.historyLimit}`;
        const response = await fetch(historyUrl, { signal: abortController.signal });
        const payload = await parseJSONResponse(
          response,
          "History endpoint",
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
        setReadings(normalized);
        setLastError("");
      } catch (error) {
        if (abortController.signal.aborted) {
          return;
        }

        const message = error instanceof Error ? error.message : "History request failed";
        setLastError(message);
      } finally {
        if (!abortController.signal.aborted) {
          setIsLoadingHistory(false);
        }
      }
    }

    loadHistory();

    return () => {
      abortController.abort();
    };
  }, [
    backendBaseUrl,
    selectedWindow.id,
    selectedWindow.historyLimit,
    selectedWindow.label,
  ]);

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
      const streamURL = new URL(`${backendBaseUrl}/api/stream`);
      eventSource = new EventSource(streamURL.toString());

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
  }, [backendBaseUrl]);

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

  useEffect(() => {
    let closed = false;
    let timer = null;
    const abortController = new AbortController();

    async function loadInsights() {
      try {
        const insightsUrl = `${backendBaseUrl}/api/insights?limit=${INSIGHT_MAX_ITEMS}`;
        const response = await fetch(insightsUrl, { signal: abortController.signal });
        const payload = await parseJSONResponse(
          response,
          "Insights endpoint",
          insightsUrl,
          backendBaseUrl
        );

        if (!response.ok) {
          const errorMessage =
            typeof payload.error === "string"
              ? payload.error
              : `insights request failed with status ${response.status}`;
          console.warn("Insights request failed", {
            status: response.status,
            error: errorMessage
          });
          throw new Error("AI insights are currently unavailable.");
        }

        if (closed) {
          return;
        }

        const sourceData = Array.isArray(payload.insights) ? payload.insights : [];
        const nextInsights = sourceData
          .map(normalizeInsight)
          .filter(Boolean)
          .slice(0, INSIGHT_MAX_ITEMS);
        const nextSource = typeof payload.source === "string" ? payload.source : "openai";
        const generatedAtRaw = Number(payload.generated_at);
        const generatedAt = Number.isFinite(generatedAtRaw) ? generatedAtRaw : 0;

        setInsights(nextInsights);
        setInsightSource(nextSource);
        setInsightsError("");
        writeInsightsCache(
          backendBaseUrl,
          nextSource,
          generatedAt,
          nextInsights
        );
      } catch (error) {
        if (closed || abortController.signal.aborted) {
          return;
        }
        const diagnostic = error instanceof Error ? error.message : "failed to load insights";
        console.error("Insights fetch error", diagnostic);
        setInsightsError("AI insights are currently unavailable.");
      } finally {
        if (!closed) {
          setIsLoadingInsights(false);
          timer = window.setTimeout(loadInsights, INSIGHT_POLL_INTERVAL_MS);
        }
      }
    }

    loadInsights();

    return () => {
      closed = true;
      abortController.abort();
      if (timer) {
        window.clearTimeout(timer);
      }
    };
  }, [backendBaseUrl]);

  useEffect(() => {
    let closed = false;
    let timer = null;
    const abortController = new AbortController();

    async function loadOpsFeed() {
      try {
        const opsFeedUrl = `${backendBaseUrl}/api/ops/events?limit=${OPS_FEED_MAX_ITEMS}`;
        const response = await fetch(opsFeedUrl, { signal: abortController.signal });
        const payload = await parseJSONResponse(
          response,
          "Ops feed endpoint",
          opsFeedUrl,
          backendBaseUrl
        );

        if (!response.ok) {
          const errorMessage =
            typeof payload.error === "string"
              ? payload.error
              : `ops feed request failed with status ${response.status}`;
          console.warn("Ops feed request failed", {
            status: response.status,
            error: errorMessage
          });
          throw new Error("Ops log is currently unavailable.");
        }

        if (closed) {
          return;
        }

        const sourceData = Array.isArray(payload.events) ? payload.events : [];
        const normalized = sourceData.map(normalizeOpsEvent).filter(Boolean);
        setFeedItems(normalized.slice(0, OPS_FEED_MAX_ITEMS));
        setFeedError("");
      } catch (error) {
        if (closed || abortController.signal.aborted) {
          return;
        }
        const diagnostic = error instanceof Error ? error.message : "failed to load ops feed";
        console.error("Ops feed fetch error", diagnostic);
        setFeedError("Ops log is currently unavailable.");
      } finally {
        if (!closed) {
          setIsLoadingFeed(false);
          timer = window.setTimeout(loadOpsFeed, OPS_FEED_POLL_INTERVAL_MS);
        }
      }
    }

    loadOpsFeed();

    return () => {
      closed = true;
      abortController.abort();
      if (timer) {
        window.clearTimeout(timer);
      }
    };
  }, [backendBaseUrl]);

  const visibleReadings = useMemo(() => {
    if (readings.length <= selectedWindow.historyLimit) {
      return readings;
    }
    return readings.slice(readings.length - selectedWindow.historyLimit);
  }, [readings, selectedWindow.historyLimit]);

  const chartReadings = useMemo(
    () => downsampleReadings(visibleReadings, selectedWindow.chartPoints),
    [visibleReadings, selectedWindow.chartPoints]
  );

  const kpis = useMemo(() => buildKpis(visibleReadings, windowId), [visibleReadings, windowId]);

  const chartData = useMemo(
    () =>
      chartReadings.map((reading) => ({
        timestamp: reading.timestamp,
        pm2: reading.pm2,
        temperature: reading.temperature
      })),
    [chartReadings]
  );
  const temperatureDomain = useMemo(
    () => computeTemperatureDomain(visibleReadings),
    [visibleReadings]
  );
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
          </div>
          <div className="topbarMeta">
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

          <div className="sideStack">
            <aside className="card panel insightsPanel">
              <div className="panelHead">
                <h2>AI Insights</h2>
                <span>{insightSource}</span>
              </div>
              {isLoadingInsights && insights.length === 0 ? (
                <p className="emptyState">Analyzing latest readings...</p>
              ) : insights.length ? (
                <ul className="insightList">
                  {insights.map((insight) => (
                    <li key={insight.id} className="insightItem">
                      <div className="insightMeta">
                        <p className="insightKind">{insightKindLabel(insight.kind)}</p>
                        <p className={`insightSeverity ${insightSeverityClassName(insight.severity)}`}>
                          {insightSeverityLabel(insight.severity)}
                        </p>
                      </div>
                      <div>
                        <p className="insightTitle">{insight.title}</p>
                        <p className="insightMessage">{insight.message}</p>
                      </div>
                    </li>
                  ))}
                </ul>
              ) : insightsError ? (
                <p className="emptyState alertError">{insightsError}</p>
              ) : (
                <p className="emptyState">
                  No active insights for the selected window.
                  {lastError ? ` Data warning: ${lastError}` : ""}
                </p>
              )}
            </aside>

            <aside className="card panel feed">
              <div className="panelHead">
                <h2>Ops Feed</h2>
                <span>Persisted backend events</span>
              </div>
              {isLoadingFeed ? (
                <p className="emptyState">Loading operations log...</p>
              ) : feedError ? (
                <p className="emptyState alertError">{feedError}</p>
              ) : feedItems.length ? (
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
              ) : (
                <p className="emptyState">No operations events yet.</p>
              )}
            </aside>
          </div>
        </section>
      </main>
    </div>
  );
}
