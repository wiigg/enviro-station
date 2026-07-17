import { INSIGHT_DIAGNOSTIC_STALE_AFTER_MS } from "./dashboardConfig";

function formatAge(timestamp, now) {
  const ageMs = Math.max(0, now - timestamp);
  if (ageMs < 10_000) {
    return "just now";
  }
  if (ageMs < 60_000) {
    return `${Math.floor(ageMs / 1000)}s ago`;
  }
  if (ageMs < 60 * 60_000) {
    return `${Math.floor(ageMs / 60_000)}m ago`;
  }
  return `${Math.floor(ageMs / (60 * 60_000))}h ago`;
}

function buildTelemetryCheck({ connectionStatus, lastError, lastReadingAt, now }) {
  const readingAge = lastReadingAt ? formatAge(lastReadingAt, now) : "not received yet";

  if (connectionStatus === "offline") {
    return {
      id: "telemetry",
      label: "Live telemetry",
      state: "error",
      summary: `Latest reading ${readingAge}; live data is stale.`,
      action:
        "Check Pi power and the sensor cable, then verify sensor.service and Tailscale/Wi-Fi."
    };
  }
  if (connectionStatus === "degraded") {
    return {
      id: "telemetry",
      label: "Live telemetry",
      state: "warn",
      summary: "The live stream is reconnecting.",
      action: "Refresh the dashboard; if it continues, check Fly health and network access."
    };
  }
  if (connectionStatus === "live" && lastError) {
    return {
      id: "telemetry",
      label: "Live telemetry",
      state: "warn",
      summary: `Live readings are current (${readingAge}), but history did not refresh.`,
      action: "Check backend readiness and the Neon connection if history remains unavailable."
    };
  }
  if (connectionStatus === "live") {
    return {
      id: "telemetry",
      label: "Live telemetry",
      state: "ok",
      summary: `Latest reading ${readingAge}.`,
      action: ""
    };
  }
  if (connectionStatus === "waiting") {
    return {
      id: "telemetry",
      label: "Live telemetry",
      state: "pending",
      summary: "Stream connected; waiting for the first reading.",
      action: ""
    };
  }
  return {
    id: "telemetry",
    label: "Live telemetry",
    state: "pending",
    summary: "Opening the live stream.",
    action: ""
  };
}

function buildParticulateCheck(particulateAvailable) {
  if (particulateAvailable === false) {
    return {
      id: "particulate",
      label: "Particle sensor",
      state: "error",
      summary: "No fresh PM readings; cached values are excluded.",
      action: "Power down the Pi, reseat the PMS5003 cable, then verify its fan is running."
    };
  }
  if (particulateAvailable === true) {
    return {
      id: "particulate",
      label: "Particle sensor",
      state: "ok",
      summary: "Fresh PM readings received.",
      action: ""
    };
  }
  return {
    id: "particulate",
    label: "Particle sensor",
    state: "pending",
    summary: "Waiting for particle sensor status.",
    action: ""
  };
}

function insightTriggerLabel(trigger) {
  if (trigger === "event") {
    return "sensor change";
  }
  if (trigger === "interval") {
    return "scheduled check";
  }
  if (trigger === "outdoor") {
    return "outdoor change";
  }
  if (trigger === "warmup" || trigger === "startup") {
    return "startup";
  }
  return "";
}

function buildInsightsCheck({
  insightGeneratedAt,
  insightTrigger,
  insights,
  insightsError,
  isLoadingInsights,
  now
}) {
  if (insightsError) {
    return {
      id: "insights",
      label: "AI insights",
      state: "warn",
      summary: "Insights are unavailable; live monitoring is unaffected.",
      action: "Check the backend AI logs and configuration if this persists."
    };
  }
  if (isLoadingInsights && !insights.length) {
    return {
      id: "insights",
      label: "AI insights",
      state: "pending",
      summary: "Checking the latest insight snapshot.",
      action: ""
    };
  }
  if (!insights.length) {
    return {
      id: "insights",
      label: "AI insights",
      state: "pending",
      summary: "Waiting for the first generated insight.",
      action: ""
    };
  }

  const generatedAt = Number(insightGeneratedAt);
  if (
    Number.isFinite(generatedAt) &&
    generatedAt > 0 &&
    now - generatedAt > INSIGHT_DIAGNOSTIC_STALE_AFTER_MS
  ) {
    return {
      id: "insights",
      label: "AI insights",
      state: "warn",
      summary: `Last refreshed ${formatAge(generatedAt, now)}.`,
      action: "Check backend AI logs; the safety refresh should run every six hours."
    };
  }

  const updated = Number.isFinite(generatedAt) && generatedAt > 0
    ? ` · updated ${formatAge(generatedAt, now)}`
    : "";
  const trigger = insightTriggerLabel(insightTrigger);
  const reason = trigger ? ` via ${trigger}` : "";

  return {
    id: "insights",
    label: "AI insights",
    state: "ok",
    summary: `Insights ready${updated}${reason}.`,
    action: ""
  };
}

function buildOperationsCheck({ feedError, feedItems, isLoadingFeed }) {
  if (feedError) {
    return {
      id: "operations",
      label: "Operations log",
      state: "warn",
      summary: "The operations endpoint did not respond.",
      action: "Refresh the dashboard; if it persists, check /api/ops/events and read access."
    };
  }
  if (isLoadingFeed) {
    return {
      id: "operations",
      label: "Operations log",
      state: "pending",
      summary: "Checking the operations endpoint.",
      action: ""
    };
  }

  const eventCount = feedItems.length;
  return {
    id: "operations",
    label: "Operations log",
    state: "ok",
    summary: eventCount
      ? `Endpoint responding · ${eventCount} recent event${eventCount === 1 ? "" : "s"}.`
      : "Endpoint responding · no recent incidents.",
    action: ""
  };
}

export function buildDashboardDiagnostics({
  connectionStatus,
  feedError,
  feedItems,
  insightGeneratedAt,
  insightTrigger,
  insights,
  insightsError,
  isLoadingFeed,
  isLoadingInsights,
  lastError,
  lastReadingAt,
  particulateAvailable,
  now = Date.now()
}) {
  const checks = [
    buildTelemetryCheck({ connectionStatus, lastError, lastReadingAt, now }),
    buildParticulateCheck(particulateAvailable),
    buildInsightsCheck({
      insightGeneratedAt,
      insightTrigger,
      insights,
      insightsError,
      isLoadingInsights,
      now
    }),
    buildOperationsCheck({ feedError, feedItems, isLoadingFeed })
  ];

  const errors = checks.filter((check) => check.state === "error").length;
  const warnings = checks.filter((check) => check.state === "warn").length;
  const pending = checks.some((check) => check.state === "pending");

  if (errors) {
    return {
      checks,
      summary: `${errors} action${errors === 1 ? "" : "s"} needed`,
      tone: "error"
    };
  }
  if (warnings) {
    return {
      checks,
      summary: `${warnings} check${warnings === 1 ? "" : "s"} need${warnings === 1 ? "s" : ""} attention`,
      tone: "warn"
    };
  }
  if (pending) {
    return { checks, summary: "Checks in progress", tone: "pending" };
  }
  return { checks, summary: "All checks passing", tone: "ok" };
}
