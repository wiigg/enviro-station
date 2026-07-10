import { memo } from "react";

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

export default memo(function InsightsCard({
  insightSource,
  insights,
  insightsError,
  isLoadingInsights,
  lastError
}) {
  return (
    <aside className="card panel insightsPanel">
      <div className="panelHead">
        <h2>AI insights</h2>
        <span>{insightSource}</span>
      </div>
      {isLoadingInsights && insights.length === 0 ? (
        <p className="emptyState" role="status">
          Analyzing latest readings...
        </p>
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
        <p className="emptyState alertError" role="alert">
          {insightsError}
        </p>
      ) : (
        <p className="emptyState">
          No active insights for the selected window.
          {lastError ? ` Data warning: ${lastError}` : ""}
        </p>
      )}
    </aside>
  );
});
