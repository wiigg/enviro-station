import { memo } from "react";

export default memo(function OpsFeedCard({ feedError, feedItems, isLoadingFeed }) {
  return (
    <aside className="card panel feed">
      <div className="panelHead">
        <h2>Ops Feed</h2>
        <span>Recent backend events</span>
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
        <p className="emptyState">No operations events yet.</p>
      )}
    </aside>
  );
});
