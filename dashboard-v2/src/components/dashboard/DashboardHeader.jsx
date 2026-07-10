import { memo } from "react";

function dashboardSummary(kpis, connectionStatus) {
  if (connectionStatus === "offline") {
    return { label: "Stale readings", tone: "alert" };
  }
  if (connectionStatus === "degraded") {
    return { label: "Reconnecting", tone: "warn" };
  }
  if (kpis.some((item) => item.state === "alert")) {
    return { label: "Action needed", tone: "alert" };
  }
  if (kpis.some((item) => item.state === "warn")) {
    return { label: "Worth watching", tone: "warn" };
  }
  if (connectionStatus === "waiting" || kpis.every((item) => item.state === "muted")) {
    return { label: "Waiting for readings", tone: "muted" };
  }
  return { label: "Environment stable", tone: "ok" };
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
    return "Data stale";
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

function formatLastReading(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown";
  }
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  });
}

const StatusChip = memo(function StatusChip({ connectionStatus }) {
  return (
    <span
      className={`chip chipStatus ${statusClassName(connectionStatus)}`}
      role="status"
      aria-live="polite"
      aria-atomic="true"
    >
      {statusLabel(connectionStatus)}
    </span>
  );
});

export default memo(function DashboardHeader({
  children,
  connectionStatus,
  deviceLabel,
  kpis,
  lastError,
  lastReadingAt
}) {
  const summary = dashboardSummary(kpis, connectionStatus);

  return (
    <header className={`dashboardHero overview-${summary.tone} reveal`}>
      <div className="heroTopbar">
        <div className="brandLockup">
          <img
            className="brandMark"
            src="/enviro-station-logo.svg"
            alt=""
            aria-hidden="true"
          />
          <div className="titleBlock">
            <p className="eyebrow">Enviro Station · {deviceLabel}</p>
            <h1>Air quality dashboard</h1>
          </div>
        </div>
        <StatusChip connectionStatus={connectionStatus} />
      </div>

      <div className="heroBody">
        <div className="overviewSummary" aria-live="polite" aria-atomic="true">
          <p className="overviewLabel">Station status</p>
          <div className="overviewTitle">
            <span className="overviewSignal" aria-hidden="true" />
            <p className="overviewValue">{summary.label}</p>
          </div>
          {lastError ? (
            <p className="overviewWarning" role="alert">
              {lastError}
            </p>
          ) : null}
        </div>

        <div className="heroTools">
          <div className="readingMeta">
            <span>Last reading</span>
            {lastReadingAt ? (
              <time dateTime={new Date(lastReadingAt).toISOString()}>
                {formatLastReading(lastReadingAt)}
              </time>
            ) : (
              <strong>No readings yet</strong>
            )}
          </div>
          {children}
        </div>
      </div>
    </header>
  );
});
