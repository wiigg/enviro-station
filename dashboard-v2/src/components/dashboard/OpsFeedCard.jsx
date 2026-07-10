import { memo } from "react";

export default memo(function OpsFeedCard({ feedError, feedItems, isLoadingFeed }) {
  return (
    <aside className="card feed diagnosticsPanel" aria-label="Operations diagnostics">
      <details>
        <summary className="diagnosticsSummary">
          <span className="diagnosticsTitle">Diagnostics</span>
          <span>Operations feed</span>
        </summary>
        <div className="diagnosticsContent">
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
            <p className="emptyState">No operations events yet.</p>
          )}
        </div>
      </details>
    </aside>
  );
});
