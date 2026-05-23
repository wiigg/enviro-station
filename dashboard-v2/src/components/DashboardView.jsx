import { memo } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis
} from "recharts";

const AXIS_TICK_STYLE = { fill: "#647184", fontSize: 11 };
const TOOLTIP_CONTENT_STYLE = {
  borderRadius: "8px",
  border: "1px solid rgba(19, 28, 43, 0.12)",
  background: "rgba(255,255,255,0.98)",
  boxShadow: "0 12px 30px rgba(19, 28, 43, 0.12)"
};
const PARTICULATE_TICK_INTERVALS = 5;
const PARTICULATE_MIN_AXIS_MAX = 4;
const PARTICULATE_MIN_TICK_STEP = 2;

const TREND_PANELS = [
  {
    title: "Particulate Trend",
    subtitle: "PM2.5 peak-aware trend",
    ariaLabel: "Particulate trend chart",
    lineDataKey: "pm2",
    lineStroke: "#f27a3e",
    averageDataKey: "pm2Average",
    averageStroke: "#4f6278",
    tooltipName: "PM2.5",
    tooltipUnit: "ug/m3",
    useParticulateYAxis: true
  },
  {
    title: "Comfort Trend",
    subtitle: "Temperature over selected window",
    ariaLabel: "Comfort trend chart",
    lineDataKey: "temperature",
    lineStroke: "#1f8a78",
    averageDataKey: "temperatureAverage",
    averageStroke: "#4f6278",
    tooltipName: "Temperature",
    tooltipUnit: "C",
    useTemperatureDomain: true
  }
];

const KPI_STATE_LABELS = {
  alert: "Action",
  warn: "Watch",
  ok: "Good",
  muted: "Waiting"
};

function dashboardSummary(kpis, connectionStatus) {
  if (connectionStatus === "offline") {
    return { label: "Offline", tone: "alert" };
  }
  if (connectionStatus === "degraded") {
    return { label: "Reconnecting", tone: "warn" };
  }
  if (kpis.some((item) => item.state === "alert")) {
    return { label: "Action needed", tone: "alert" };
  }
  if (kpis.some((item) => item.state === "warn")) {
    return { label: "Watch", tone: "warn" };
  }
  if (connectionStatus === "waiting" || kpis.every((item) => item.state === "muted")) {
    return { label: "Waiting", tone: "muted" };
  }
  return { label: "Stable", tone: "ok" };
}

function statusLabel(status) {
  if (status === "live") {
    return "Connected";
  }
  if (status === "waiting") {
    return "Waiting for data";
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
  if (status === "waiting") {
    return "statusWaiting";
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
    return "Action";
  }
  if (severity === "warn") {
    return "Watch";
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

function particulateAxisTicks(chartData, keys) {
  let max = 0;
  for (const point of chartData) {
    for (const key of keys) {
      const value = point[key];
      if (Number.isFinite(value) && value > max) {
        max = value;
      }
    }
  }

  const paddedMax = Math.max(max * 1.15, PARTICULATE_MIN_AXIS_MAX);
  const step = Math.max(
    PARTICULATE_MIN_TICK_STEP,
    niceAxisStep(paddedMax / PARTICULATE_TICK_INTERVALS)
  );
  const axisMax = Math.ceil(paddedMax / step) * step;
  const tickCount = Math.round(axisMax / step);
  return Array.from(
    { length: tickCount + 1 },
    (_item, index) => step * index
  );
}

function niceAxisStep(value) {
  const magnitude = 10 ** Math.floor(Math.log10(value));
  const fraction = value / magnitude;
  if (fraction <= 1.5) {
    return magnitude;
  }
  if (fraction <= 3) {
    return 2 * magnitude;
  }
  if (fraction <= 7) {
    return 5 * magnitude;
  }
  return 10 * magnitude;
}

const StatusChip = memo(function StatusChip({ connectionStatus }) {
  return (
    <span className={`chip chipStatus ${statusClassName(connectionStatus)}`}>
      {statusLabel(connectionStatus)}
    </span>
  );
});

const WindowControls = memo(function WindowControls({
  onSelectWindow,
  selectedWindowId,
  windowOptions
}) {
  return (
    <section className="controls reveal" aria-label="Dashboard time range">
      <div className="controlGroup" role="tablist" aria-label="Time range">
        {windowOptions.map((windowOption) => (
          <button
            key={windowOption.id}
            className={`segmentButton ${windowOption.id === selectedWindowId ? "segmentActive" : ""}`}
            type="button"
            role="tab"
            aria-selected={windowOption.id === selectedWindowId}
            onClick={() => onSelectWindow(windowOption.id)}
          >
            {windowOption.label}
          </button>
        ))}
      </div>
    </section>
  );
});

const KpiGrid = memo(function KpiGrid({ kpis }) {
  return (
    <section className="kpiGrid reveal">
      {kpis.map((item) => (
        <article className={`card kpi kpi-${item.state}`} key={item.label}>
          <div className="kpiHead">
            <span>{item.label}</span>
            <span className={`statePill state-${item.state}`}>
              {KPI_STATE_LABELS[item.state] ?? item.state}
            </span>
          </div>
          <p className="kpiValue">
            {item.value}
            <span>{item.unit}</span>
          </p>
          <p className="kpiTrend">{item.trend}</p>
        </article>
      ))}
    </section>
  );
});

const TrendPanel = memo(function TrendPanel({
  title,
  subtitle,
  ariaLabel,
  chartData,
  axisTickFormatter,
  yAxisDomain,
  useParticulateYAxis = false,
  lineDataKey,
  lineStroke,
  averageDataKey,
  averageStroke,
  tooltipName,
  tooltipUnit
}) {
  const yAxisTicks = useParticulateYAxis
    ? particulateAxisTicks(chartData, [lineDataKey, averageDataKey])
    : undefined;
  const resolvedYAxisDomain = yAxisTicks
    ? [0, yAxisTicks[yAxisTicks.length - 1]]
    : yAxisDomain;

  return (
    <article className="card panel">
      <div className="panelHead">
        <div className="panelTitle">
          <h2>{title}</h2>
          <span>{subtitle}</span>
        </div>
        <div className="chartLegend" aria-hidden="true">
          <span>
            <i style={{ backgroundColor: lineStroke }} />
            Reading
          </span>
          <span>
            <i className="averageLegend" style={{ backgroundColor: averageStroke }} />
            Avg
          </span>
        </div>
      </div>
      {chartData.length ? (
        <div className="chart" role="img" aria-label={ariaLabel}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 8, right: 10, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="2 6" stroke="rgba(19, 28, 43, 0.1)" />
              <XAxis
                dataKey="timestamp"
                tickFormatter={axisTickFormatter}
                minTickGap={26}
                tick={AXIS_TICK_STYLE}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                allowDataOverflow={useParticulateYAxis}
                domain={resolvedYAxisDomain}
                ticks={yAxisTicks}
                width={44}
                tick={AXIS_TICK_STYLE}
                axisLine={false}
                tickLine={false}
              />
              <Tooltip
                labelFormatter={formatChartLabel}
                formatter={(value, name) => [
                  `${Number(value).toFixed(1)} ${tooltipUnit}`,
                  name
                ]}
                contentStyle={TOOLTIP_CONTENT_STYLE}
              />
              <Line
                type="linear"
                dataKey={averageDataKey}
                name={`${tooltipName} avg`}
                stroke={averageStroke}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
                connectNulls
                isAnimationActive={false}
              />
              <Line
                type="linear"
                dataKey={lineDataKey}
                name={tooltipName}
                stroke={lineStroke}
                strokeWidth={2.5}
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
  );
});

const InsightsCard = memo(function InsightsCard({
  insightSource,
  insights,
  insightsError,
  isLoadingInsights,
  lastError
}) {
  return (
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
  );
});

const OpsFeedCard = memo(function OpsFeedCard({ feedError, feedItems, isLoadingFeed }) {
  return (
    <aside className="card panel feed">
      <div className="panelHead">
        <h2>Ops Feed</h2>
        <span>Recent backend events</span>
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
  );
});

export default function DashboardView({
  axisTickFormatter,
  chartData,
  connectionStatus,
  feedError,
  feedItems,
  insightSource,
  insights,
  insightsError,
  isLoadingFeed,
  isLoadingInsights,
  kpis,
  lastError,
  onSelectWindow,
  selectedWindow,
  temperatureDomain,
  windowOptions
}) {
  const summary = dashboardSummary(kpis, connectionStatus);

  return (
    <div className="shell">
      <main className="layout">
        <header className="topbar reveal">
          <div className="brandLockup">
            <img
              className="brandMark"
              src="/enviro-station-logo.svg"
              alt=""
              aria-hidden="true"
            />
            <div className="titleBlock">
              <p className="eyebrow">Enviro Station</p>
              <h1>Air quality dashboard</h1>
            </div>
          </div>
          <div className="topbarMeta">
            <StatusChip connectionStatus={connectionStatus} />
            <span className="chip">{selectedWindow.label}</span>
          </div>
        </header>

        <section className={`overviewStrip overview-${summary.tone} reveal`}>
          <div>
            <p className="overviewLabel">Current state</p>
            <p className="overviewValue">{summary.label}</p>
          </div>
          {lastError ? <p className="overviewWarning">{lastError}</p> : null}
        </section>

        <WindowControls
          onSelectWindow={onSelectWindow}
          selectedWindowId={selectedWindow.id}
          windowOptions={windowOptions}
        />

        <KpiGrid kpis={kpis} />

        <section className="dashboardGrid reveal">
          <div className="trendStack">
            {TREND_PANELS.map((panel) => (
              <TrendPanel
                key={panel.lineDataKey}
                title={panel.title}
                subtitle={panel.subtitle}
                ariaLabel={panel.ariaLabel}
                chartData={chartData}
                axisTickFormatter={axisTickFormatter}
                yAxisDomain={panel.useTemperatureDomain ? temperatureDomain : panel.yAxisDomain}
                useParticulateYAxis={panel.useParticulateYAxis}
                lineDataKey={panel.lineDataKey}
                lineStroke={panel.lineStroke}
                averageDataKey={panel.averageDataKey}
                averageStroke={panel.averageStroke}
                tooltipName={panel.tooltipName}
                tooltipUnit={panel.tooltipUnit}
              />
            ))}
          </div>

          <div className="sideStack">
            <InsightsCard
              insightSource={insightSource}
              insights={insights}
              insightsError={insightsError}
              isLoadingInsights={isLoadingInsights}
              lastError={lastError}
            />
            <OpsFeedCard
              feedError={feedError}
              feedItems={feedItems}
              isLoadingFeed={isLoadingFeed}
            />
          </div>
        </section>
      </main>
    </div>
  );
}
