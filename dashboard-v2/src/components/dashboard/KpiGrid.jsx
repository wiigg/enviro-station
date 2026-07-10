import { memo } from "react";

const KPI_STATE_LABELS = {
  alert: "Action",
  warn: "Watch",
  ok: "Good",
  muted: "Waiting"
};

export default memo(function KpiGrid({ kpis }) {
  return (
    <section className="kpiGrid reveal" aria-label="Current readings">
      {kpis.map((item) => (
        <article className={`card kpi kpi-${item.state}`} key={item.label}>
          <div className="kpiHead">
            <span className="kpiLabel">
              <i className="kpiSignal" aria-hidden="true" />
              {item.label}
            </span>
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
