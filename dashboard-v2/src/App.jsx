const kpis = [
  { label: "PM2.5", value: "12.4", unit: "ug/m3", trend: "+3.2%", state: "warn" },
  { label: "PM10", value: "19.8", unit: "ug/m3", trend: "-1.4%", state: "ok" },
  { label: "Temp", value: "22.9", unit: "C", trend: "+0.6", state: "ok" },
  { label: "Humidity", value: "41", unit: "%", trend: "-0.9%", state: "ok" }
];

const incidents = [
  {
    time: "09:42",
    title: "Batch flush completed",
    detail: "102 queued readings persisted to backend"
  },
  {
    time: "09:18",
    title: "PM2.5 crossed soft threshold",
    detail: "Above 10 ug/m3 for 11 minutes"
  },
  {
    time: "08:55",
    title: "Sensor heartbeat resumed",
    detail: "Edge device ES-004 reconnected"
  }
];

const particulateSeries = [8, 9, 11, 13, 15, 14, 12, 10, 11, 12, 14, 13];
const comfortSeries = [20, 21, 22, 24, 23, 22, 21, 20, 21, 22, 23, 24];

function pathFromSeries(series, width, height, inset) {
  const min = Math.min(...series);
  const max = Math.max(...series);
  const range = Math.max(1, max - min);
  const step = (width - inset * 2) / (series.length - 1);

  return series
    .map((value, index) => {
      const x = inset + index * step;
      const y = height - inset - ((value - min) / range) * (height - inset * 2);
      return `${index === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(" ");
}

export default function App() {
  const particulatePath = pathFromSeries(particulateSeries, 560, 190, 14);
  const comfortPath = pathFromSeries(comfortSeries, 560, 190, 14);

  return (
    <div className="shell">
      <div className="background" aria-hidden="true" />
      <main className="layout">
        <header className="card topbar reveal">
          <div>
            <p className="eyebrow">Enviro Station</p>
            <h1>Air Quality Control Deck</h1>
            <p className="subtitle">
              Phase 1 focuses on a fresh visual baseline and information layout.
            </p>
          </div>
          <div className="topbarMeta">
            <span className="chip chipPrimary">v2 Prototype</span>
            <span className="chip">Static Mode</span>
          </div>
        </header>

        <section className="card controls reveal">
          <div className="controlGroup">
            <button className="btn btnActive" type="button">
              Live
            </button>
            <button className="btn" type="button">
              1h
            </button>
            <button className="btn" type="button">
              24h
            </button>
            <button className="btn" type="button">
              7d
            </button>
          </div>
          <p className="hint">Data and stream wiring lands in Phase 2.</p>
        </section>

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
              <p className="kpiTrend">{item.trend} vs previous window</p>
            </article>
          ))}
        </section>

        <section className="dataGrid reveal">
          <article className="card panel">
            <div className="panelHead">
              <h2>Particulate Trend</h2>
              <span>PM focus</span>
            </div>
            <svg viewBox="0 0 560 190" className="chart" role="img" aria-label="Particulate trend chart">
              <path d={particulatePath} className="line lineHot" />
            </svg>
          </article>

          <article className="card panel">
            <div className="panelHead">
              <h2>Comfort Trend</h2>
              <span>Temp + humidity</span>
            </div>
            <svg viewBox="0 0 560 190" className="chart" role="img" aria-label="Comfort trend chart">
              <path d={comfortPath} className="line lineCool" />
            </svg>
          </article>

          <aside className="card panel feed">
            <div className="panelHead">
              <h2>Ops Feed</h2>
              <span>Recent events</span>
            </div>
            <ul>
              {incidents.map((incident) => (
                <li key={`${incident.time}-${incident.title}`}>
                  <p className="time">{incident.time}</p>
                  <div>
                    <p className="event">{incident.title}</p>
                    <p className="detail">{incident.detail}</p>
                  </div>
                </li>
              ))}
            </ul>
          </aside>
        </section>
      </main>
    </div>
  );
}

