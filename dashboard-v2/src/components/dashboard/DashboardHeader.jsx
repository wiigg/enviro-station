import { memo } from "react";

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
  connectionStatus,
  deviceLabel,
  kpis,
  lastError,
  lastReadingAt,
  selectedWindow
}) {
  const summary = dashboardSummary(kpis, connectionStatus);

  return (
    <>
      <header className="topbar reveal">
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
        <div className="topbarMeta">
          <StatusChip connectionStatus={connectionStatus} />
          <span className="chip">{selectedWindow.label}</span>
          {lastReadingAt ? (
            <span className="chip readingChip">
              <span>Last reading</span>
              <time dateTime={new Date(lastReadingAt).toISOString()}>
                {formatLastReading(lastReadingAt)}
              </time>
            </span>
          ) : (
            <span className="chip">No readings yet</span>
          )}
        </div>
      </header>

      <section
        className={`overviewStrip overview-${summary.tone} reveal`}
        aria-live="polite"
        aria-atomic="true"
      >
        <div>
          <p className="overviewLabel">Current state</p>
          <p className="overviewValue">{summary.label}</p>
        </div>
        {lastError ? (
          <p className="overviewWarning" role="alert">
            {lastError}
          </p>
        ) : null}
      </section>
    </>
  );
});
