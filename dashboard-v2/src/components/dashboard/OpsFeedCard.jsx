import { memo } from "react";

const diagnosticStateLabel = {
  error: "Action needed",
  ok: "Working",
  pending: "Checking",
  warn: "Check"
};

export default memo(function OpsFeedCard({
  diagnosticChecks,
  diagnosticsSummary,
  diagnosticsTone,
  feedError,
  feedItems,
  isLoadingFeed
}) {
  return (
    <aside className="card feed diagnosticsPanel" aria-label="Operations diagnostics">
      <details>
        <summary className="diagnosticsSummary">
          <span className="diagnosticsTitle">Diagnostics</span>
          <span className={`diagnosticsSummaryState diagnosticState-${diagnosticsTone}`}>
            {diagnosticsSummary}
          </span>
        </summary>
        <div className="diagnosticsContent">
          <ul className="diagnosticChecks" aria-label="System checks">
            {diagnosticChecks.map((check) => (
              <li className="diagnosticCheck" key={check.id}>
                <span
                  className={`diagnosticSignal diagnosticState-${check.state}`}
                  aria-hidden="true"
                />
                <div className="diagnosticCopy">
                  <div className="diagnosticLabelRow">
                    <p className="event">{check.label}</p>
                    <span className={`diagnosticStateLabel diagnosticState-${check.state}`}>
                      {diagnosticStateLabel[check.state]}
                    </span>
                  </div>
                  <p className="detail">{check.summary}</p>
                  {check.action ? (
                    <p className="diagnosticAction">
                      <strong>Next:</strong> {check.action}
                    </p>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>

          <div className="diagnosticEventsHeader">
            <p>Recent operations</p>
            {feedItems.length ? <span>{feedItems.length} shown</span> : null}
          </div>
          {isLoadingFeed ? (
            <p className="emptyState" role="status">
              Loading operations log...
            </p>
          ) : feedError ? (
            <p className="emptyState alertError" role="alert">
              {feedError}
            </p>
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
            <p className="emptyState">No recent restarts or connection changes.</p>
          )}
        </div>
      </details>
    </aside>
  );
});
