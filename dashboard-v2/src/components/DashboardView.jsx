import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis
} from "recharts";

const AXIS_TICK_STYLE = { fill: "#5d6884", fontSize: 11 };
const TOOLTIP_CONTENT_STYLE = {
  borderRadius: "12px",
  border: "1px solid rgba(24, 35, 65, 0.12)",
  background: "rgba(255,255,255,0.96)"
};

const TREND_PANELS = [
  {
    title: "Particulate Trend",
    subtitle: "PM2.5 over selected window",
    ariaLabel: "Particulate trend chart",
    lineDataKey: "pm2",
    lineStroke: "#f27a3e",
    tooltipName: "PM2.5",
    tooltipUnit: "ug/m3"
  },
  {
    title: "Comfort Trend",
    subtitle: "Temperature over selected window",
    ariaLabel: "Comfort trend chart",
    lineDataKey: "temperature",
    lineStroke: "#1f8a78",
    tooltipName: "Temperature",
    tooltipUnit: "C",
    useTemperatureDomain: true
  }
];

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

function StatusChip({ connectionStatus }) {
  return (
    <span className={`chip chipStatus ${statusClassName(connectionStatus)}`}>
      {statusLabel(connectionStatus)}
    </span>
  );
}

function WindowControls({ onSelectWindow, selectedWindowId, windowOptions }) {
  return (
    <section className="card controls reveal">
      <div className="controlGroup">
        {windowOptions.map((windowOption) => (
          <button
            key={windowOption.id}
            className={`btn ${windowOption.id === selectedWindowId ? "btnActive" : ""}`}
            type="button"
            onClick={() => onSelectWindow(windowOption.id)}
          >
            {windowOption.label}
          </button>
        ))}
      </div>
    </section>
  );
}

function KpiGrid({ kpis }) {
  return (
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
  );
}

function TrendPanel({
  title,
  subtitle,
  ariaLabel,
  chartData,
  axisTickFormatter,
  yAxisDomain,
  lineDataKey,
  lineStroke,
  tooltipName,
  tooltipUnit
}) {
  return (
    <article className="card panel">
      <div className="panelHead">
        <h2>{title}</h2>
        <span>{subtitle}</span>
      </div>
      {chartData.length ? (
        <div className="chart" role="img" aria-label={ariaLabel}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 8, right: 12, left: 0, bottom: 6 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(24, 35, 65, 0.12)" />
              <XAxis
                dataKey="timestamp"
                tickFormatter={axisTickFormatter}
                minTickGap={26}
                tick={AXIS_TICK_STYLE}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                domain={yAxisDomain}
                width={44}
                tick={AXIS_TICK_STYLE}
                axisLine={false}
                tickLine={false}
              />
              <Tooltip
                labelFormatter={formatChartLabel}
                formatter={(value) => [`${Number(value).toFixed(1)} ${tooltipUnit}`, tooltipName]}
                contentStyle={TOOLTIP_CONTENT_STYLE}
              />
              <Line
                type="monotone"
                dataKey={lineDataKey}
                stroke={lineStroke}
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
  );
}

function InsightsCard({
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
}

function OpsFeedCard({ feedError, feedItems, isLoadingFeed }) {
  return (
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
  );
}

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
            <StatusChip connectionStatus={connectionStatus} />
          </div>
        </header>

        <WindowControls
          onSelectWindow={onSelectWindow}
          selectedWindowId={selectedWindow.id}
          windowOptions={windowOptions}
        />

        <KpiGrid kpis={kpis} />

        <section className="dataGrid reveal">
          {TREND_PANELS.map((panel) => (
            <TrendPanel
              key={panel.lineDataKey}
              title={panel.title}
              subtitle={panel.subtitle}
              ariaLabel={panel.ariaLabel}
              chartData={chartData}
              axisTickFormatter={axisTickFormatter}
              yAxisDomain={panel.useTemperatureDomain ? temperatureDomain : undefined}
              lineDataKey={panel.lineDataKey}
              lineStroke={panel.lineStroke}
              tooltipName={panel.tooltipName}
              tooltipUnit={panel.tooltipUnit}
            />
          ))}

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
